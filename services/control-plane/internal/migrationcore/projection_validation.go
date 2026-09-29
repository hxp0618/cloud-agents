package migrationcore

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	signedIntegerDecimalPattern = regexp.MustCompile(`^(0|-?[1-9][0-9]{0,18})$`)
	exactNumericPattern         = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)
	ryuDecimalPattern           = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?(e-?(0|[1-9][0-9]*))?$`)
)

var privilegeOrder = map[string]int{
	"CONNECT": 0, "CREATE": 1, "DELETE": 2, "EXECUTE": 3,
	"INSERT": 4, "REFERENCES": 5, "SELECT": 6, "TEMPORARY": 7,
	"TRIGGER": 8, "TRUNCATE": 9, "UPDATE": 10, "USAGE": 11,
}

func (set ACLSetProjection) Validate() error {
	if set.Entries == nil {
		return invalidProjection("acl-set", "ACL entries must be an explicit array")
	}
	switch set.CatalogValue {
	case "null":
		if len(set.Entries) != 0 {
			return invalidProjection("acl-set", "null catalog ACL must have no entries")
		}
	case "explicit":
	default:
		return invalidProjection("acl-set", "unknown catalog ACL value")
	}
	keys := make([]string, len(set.Entries))
	for index, entry := range set.Entries {
		if entry.Grantor == "" || entry.Grantee == "" || entry.Privileges == nil || entry.Grantable == nil {
			return invalidProjection("acl-set", "ACL provenance and arrays are required")
		}
		switch entry.Origin {
		case "catalog_explicit", "owner_implicit", "public_default", "default_acl_catalog":
		default:
			return invalidProjection("acl-set", "unknown ACL origin")
		}
		if err := validatePrivilegeList(entry.Privileges); err != nil {
			return err
		}
		if err := validatePrivilegeList(entry.Grantable); err != nil {
			return err
		}
		granted := make(map[string]struct{}, len(entry.Privileges))
		for _, privilege := range entry.Privileges {
			granted[privilege] = struct{}{}
		}
		for _, privilege := range entry.Grantable {
			if _, ok := granted[privilege]; !ok {
				return invalidProjection("acl-set", "grantable privilege is absent from privileges")
			}
		}
		keys[index] = entry.Grantor + "\x00" + entry.Grantee
	}
	if !strictlySorted(keys) {
		return invalidProjection("acl-set", "ACL entries are not strictly sorted")
	}
	return nil
}

func validatePrivilegeList(privileges []string) error {
	previousRank := -1
	for index, privilege := range privileges {
		rank, ok := privilegeOrder[privilege]
		if !ok || (index > 0 && previousRank >= rank) {
			return invalidProjection("acl-set", "privilege list contains unknown, duplicate, or unsorted values")
		}
		previousRank = rank
	}
	return nil
}

func validateACLOrigins(entries []ACLProjection, allowed ...string) error {
	allowlist := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		allowlist[origin] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := allowlist[entry.Origin]; !ok {
			return invalidProjection("acl-set", "ACL origin is invalid for this projection surface")
		}
	}
	return nil
}

func validateACLPrivileges(entries []ACLProjection, allowed ...string) error {
	allowlist := make(map[string]struct{}, len(allowed))
	for _, privilege := range allowed {
		allowlist[privilege] = struct{}{}
	}
	for _, entry := range entries {
		for _, privileges := range [][]string{entry.Privileges, entry.Grantable} {
			for _, privilege := range privileges {
				if _, ok := allowlist[privilege]; !ok {
					return invalidProjection("acl-set", "privilege is invalid for this projection surface")
				}
			}
		}
	}
	return nil
}

func (identity ObjectIdentityProjection) branch() (any, error) {
	branches := []any{identity.Schema, identity.Relation, identity.Column, identity.Index, identity.Policy, identity.Type, identity.Extension, identity.Collation, identity.Opclass, identity.Function, identity.Operator, identity.Cast, identity.Constraint, identity.Trigger, identity.Internal}
	var selected any
	count := 0
	for _, branch := range branches {
		if !isNilInterface(branch) {
			selected = branch
			count++
		}
	}
	if count != 1 {
		return nil, invalidProjection("object-identity", "object identity must contain exactly one branch")
	}
	return selected, nil
}

func isNilInterface(value any) bool {
	switch typed := value.(type) {
	case *SchemaObjectIdentity:
		return typed == nil
	case *RelationObjectIdentity:
		return typed == nil
	case *ColumnObjectIdentity:
		return typed == nil
	case *IndexObjectIdentity:
		return typed == nil
	case *PolicyObjectIdentity:
		return typed == nil
	case *TypeObjectIdentity:
		return typed == nil
	case *ExtensionObjectIdentity:
		return typed == nil
	case *CollationObjectIdentity:
		return typed == nil
	case *OpclassObjectIdentity:
		return typed == nil
	case *SQLObjectIdentity:
		return typed == nil
	case *CastObjectIdentity:
		return typed == nil
	case *ConstraintObjectIdentity:
		return typed == nil
	case *TriggerObjectIdentity:
		return typed == nil
	case *InternalObjectIdentity:
		return typed == nil
	default:
		return true
	}
}

func (identity ObjectIdentityProjection) Validate() error {
	branch, err := identity.branch()
	if err != nil {
		return err
	}
	var kind string
	switch typed := branch.(type) {
	case *SchemaObjectIdentity:
		kind = typed.Kind
		if typed.Name == "" {
			return invalidProjection("object-identity", "schema name is empty")
		}
	case *RelationObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *ColumnObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "column name is empty")
		}
	case *IndexObjectIdentity:
		kind = typed.Kind
		err = firstError(typed.Identity.Validate(), typed.Relation.Validate())
	case *PolicyObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "policy name is empty")
		}
	case *TypeObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *ExtensionObjectIdentity:
		kind = typed.Kind
		if typed.Name == "" {
			err = invalidProjection("object-identity", "extension name is empty")
		}
	case *CollationObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *OpclassObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
		if typed.AccessMethod == "" {
			err = invalidProjection("object-identity", "opclass access method is empty")
		}
	case *SQLObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *CastObjectIdentity:
		kind = typed.Kind
		err = firstError(typed.SourceType.Validate(), typed.TargetType.Validate())
	case *ConstraintObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "constraint name is empty")
		}
	case *TriggerObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "trigger name is empty")
		}
		if typed.OwningConstraint != nil {
			if typed.OwningConstraint.Kind != "constraint" {
				err = invalidProjection("object-identity", "trigger owner is not a constraint identity")
			} else if ownerErr := typed.OwningConstraint.Relation.Validate(); ownerErr != nil || typed.OwningConstraint.Name == "" {
				err = invalidProjection("object-identity", "trigger owning constraint identity is incomplete")
			}
		}
	case *InternalObjectIdentity:
		kind = typed.Kind
		if typed.SemanticKind == "" {
			err = invalidProjection("object-identity", "internal semantic kind is empty")
		}
		if typed.OwningObject.Internal != nil {
			err = invalidProjection("object-identity", "internal identity cannot own another internal identity")
		}
		if ownerErr := typed.OwningObject.Validate(); err == nil {
			err = ownerErr
		}
	}
	if err != nil {
		return err
	}
	if kind == "" || kind != identity.Kind() {
		return invalidProjection("object-identity", "object identity discriminator differs from its branch")
	}
	return nil
}

func (identity ObjectIdentityProjection) Kind() string {
	switch {
	case identity.Schema != nil:
		return "schema"
	case identity.Relation != nil:
		return "relation"
	case identity.Column != nil:
		return "column"
	case identity.Index != nil:
		return "index"
	case identity.Policy != nil:
		return "policy"
	case identity.Type != nil:
		return "type"
	case identity.Extension != nil:
		return "extension"
	case identity.Collation != nil:
		return "collation"
	case identity.Opclass != nil:
		return "opclass"
	case identity.Function != nil:
		return "function"
	case identity.Operator != nil:
		return "operator"
	case identity.Cast != nil:
		return "cast"
	case identity.Constraint != nil:
		return "constraint"
	case identity.Trigger != nil:
		return "trigger"
	case identity.Internal != nil:
		return "internal"
	default:
		return ""
	}
}

func (identity TypeIdentity) Validate() error {
	if identity.Schema == "" || identity.Name == "" {
		return invalidProjection("object-identity", "type identity is incomplete")
	}
	return nil
}

func (identity SQLIdentity) Validate() error {
	if identity.Schema == "" || identity.Name == "" || identity.Arguments == nil {
		return invalidProjection("object-identity", "SQL identity is incomplete")
	}
	for _, argument := range identity.Arguments {
		if err := argument.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectIdentityClosure(identities []ObjectIdentityProjection) error {
	if identities == nil {
		return invalidProjection("object-identity", "declared object closure must be an explicit array")
	}
	keys := make([]string, len(identities))
	for index, identity := range identities {
		if err := identity.Validate(); err != nil {
			return err
		}
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		keys[index] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("object-identity", "declared object closure is duplicate or not canonically sorted")
	}
	return nil
}

func (scope ProjectionScope) Validate() error {
	if err := validateObjectIdentityClosure(scope.DeclaredObjects); err != nil {
		return err
	}
	switch scope.ScopeKind {
	case "predecessor":
		if scope.SchemaHead != nil || scope.MigrationID == nil || !migrationIDPattern.MatchString(*scope.MigrationID) || scope.ThroughStatementIndex != nil {
			return invalidProjection("projection-scope", "invalid predecessor scope")
		}
	case "statement_prefix":
		if scope.SchemaHead != nil || scope.MigrationID == nil || !migrationIDPattern.MatchString(*scope.MigrationID) || scope.ThroughStatementIndex == nil {
			return invalidProjection("projection-scope", "invalid statement prefix scope")
		}
	case "final":
		if scope.SchemaHead == nil || !migrationIDPattern.MatchString(*scope.SchemaHead) || scope.MigrationID != nil || scope.ThroughStatementIndex != nil {
			return invalidProjection("projection-scope", "invalid final scope")
		}
	default:
		return invalidProjection("projection-scope", "unknown projection scope kind")
	}
	return nil
}

func (state CatalogStateProjection) Validate() error {
	switch {
	case state.Absent != nil && state.Present == nil:
		if state.Absent.State != "schema_absent" || state.Absent.Schema != "cloud_agents" || state.Absent.Scope.ScopeKind != "predecessor" || len(state.Absent.Scope.DeclaredObjects) != 0 {
			return invalidProjection("catalog-state", "invalid schema_absent branch")
		}
		return state.Absent.Scope.Validate()
	case state.Absent == nil && state.Present != nil:
		if state.Present.State != "schema_present" {
			return invalidProjection("catalog-state", "invalid schema_present branch")
		}
		if err := state.Present.Scope.Validate(); err != nil {
			return err
		}
		if err := state.Present.Body.Validate(); err != nil {
			return err
		}
		if !equalObjectIdentityClosures(state.Present.Scope.DeclaredObjects, state.Present.Body.DeclaredObjects) {
			return invalidProjection("catalog-state", "scope and body declared object closures differ")
		}
		return nil
	default:
		return invalidProjection("catalog-state", "catalog state must contain exactly one branch")
	}
}

func (body CatalogProjectionBody) Validate() error {
	if body.Schema.Name != "cloud_agents" || body.Schema.Owner == "" || body.Schema.EffectiveACL == nil || body.Schema.SecurityLabels == nil || body.DefaultACL == nil || body.Relations == nil || body.Functions == nil || body.Dependencies == nil || body.DeclaredObjects == nil || body.DeniedObjects == nil {
		return invalidProjection("catalog-projection", "catalog projection body is sparse")
	}
	if err := body.Schema.ExplicitACL.Validate(); err != nil {
		return err
	}
	if err := validateACLOrigins(body.Schema.ExplicitACL.Entries, "catalog_explicit"); err != nil {
		return err
	}
	if err := validateACLPrivileges(body.Schema.ExplicitACL.Entries, "CREATE", "USAGE"); err != nil {
		return err
	}
	if err := (ACLSetProjection{CatalogValue: "explicit", Entries: body.Schema.EffectiveACL}).Validate(); err != nil {
		return err
	}
	if err := validateACLOrigins(body.Schema.EffectiveACL, "catalog_explicit", "owner_implicit", "public_default"); err != nil {
		return err
	}
	if err := validateACLPrivileges(body.Schema.EffectiveACL, "CREATE", "USAGE"); err != nil {
		return err
	}
	if err := validateObjectIdentityClosure(body.DeclaredObjects); err != nil {
		return err
	}
	if body.ObjectCount != uint32(len(body.DeclaredObjects)) {
		return invalidProjection("catalog-projection", "object_count differs from the declared object closure")
	}
	if err := validateRelationProjections(body.Relations); err != nil {
		return err
	}
	if err := validateFunctionProjections(body.Functions); err != nil {
		return err
	}
	if err := validateProjectedDeclaredCoverage(body); err != nil {
		return err
	}
	labelKeys := make([]string, len(body.Schema.SecurityLabels))
	for index, label := range body.Schema.SecurityLabels {
		if label.Provider == "" || label.Label == "" {
			return invalidProjection("catalog-projection", "security label is incomplete")
		}
		labelKeys[index] = label.Provider
	}
	if !strictlySorted(labelKeys) {
		return invalidProjection("catalog-projection", "security labels are duplicate or unsorted")
	}
	defaultACLKeys := make([]string, len(body.DefaultACL))
	for index, acl := range body.DefaultACL {
		if acl.Owner == "" {
			return invalidProjection("catalog-projection", "default ACL scope is incomplete")
		}
		schemaSortKey := "0"
		if acl.Schema != nil {
			if *acl.Schema != "cloud_agents" {
				return invalidProjection("catalog-projection", "default ACL schema is outside the global or cloud_agents scope")
			}
			schemaSortKey = "1" + *acl.Schema
		}
		switch acl.ObjectKind {
		case "table", "sequence", "function", "type", "schema":
		default:
			return invalidProjection("catalog-projection", "unknown default ACL object kind")
		}
		if acl.ObjectKind == "schema" && acl.Schema != nil {
			return invalidProjection("catalog-projection", "schema default ACL kind is only valid in global scope")
		}
		if acl.ACL.CatalogValue != "explicit" {
			return invalidProjection("catalog-projection", "projected default ACL catalog_value must be explicit")
		}
		if err := acl.ACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(acl.ACL.Entries, "default_acl_catalog"); err != nil {
			return err
		}
		var allowed []string
		switch acl.ObjectKind {
		case "table":
			allowed = []string{"DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"}
		case "sequence":
			allowed = []string{"SELECT", "UPDATE", "USAGE"}
		case "function":
			allowed = []string{"EXECUTE"}
		case "type":
			allowed = []string{"USAGE"}
		case "schema":
			allowed = []string{"CREATE", "USAGE"}
		}
		if err := validateACLPrivileges(acl.ACL.Entries, allowed...); err != nil {
			return err
		}
		defaultACLKeys[index] = acl.Owner + "\x00" + schemaSortKey + "\x00" + acl.ObjectKind
	}
	if !strictlySorted(defaultACLKeys) {
		return invalidProjection("catalog-projection", "default ACL projections are duplicate or unsorted")
	}
	dependencyKeys := make([]string, len(body.Dependencies))
	for index, dependency := range body.Dependencies {
		if dependency.DependencyKind == "" {
			return invalidProjection("catalog-projection", "dependency kind is empty")
		}
		if err := dependency.Depender.Validate(); err != nil {
			return err
		}
		if err := dependency.DependedOn.Validate(); err != nil {
			return err
		}
		dependerKey, err := canonicalContractKey(dependency.Depender)
		if err != nil {
			return err
		}
		dependedKey, err := canonicalContractKey(dependency.DependedOn)
		if err != nil {
			return err
		}
		dependencyKeys[index] = dependerKey + "\x00" + dependedKey + "\x00" + dependency.DependencyKind
	}
	if !strictlySorted(dependencyKeys) {
		return invalidProjection("catalog-projection", "dependencies are duplicate or unsorted")
	}
	deniedKeys := make([]string, len(body.DeniedObjects))
	for index, denied := range body.DeniedObjects {
		if err := denied.Object.Validate(); err != nil {
			return err
		}
		if denied.Owner != nil && *denied.Owner == "" {
			return invalidProjection("catalog-projection", "non-null denied object owner must be non-empty")
		}
		if denied.DependencyKind != nil && *denied.DependencyKind == "" {
			return invalidProjection("catalog-projection", "non-null denied dependency kind must be non-empty")
		}
		switch denied.ReasonCode {
		case "undeclared_object", "unsupported_object_kind", "unbound_internal_object", "dependency_outside_closure":
		default:
			return invalidProjection("catalog-projection", "denied object reason code is outside the closed set")
		}
		if denied.DependedOn != nil {
			if err := denied.DependedOn.Validate(); err != nil {
				return err
			}
		}
		deniedKey, err := canonicalContractKey(denied.Object)
		if err != nil {
			return err
		}
		deniedKeys[index] = deniedKey
	}
	if !strictlySorted(deniedKeys) {
		return invalidProjection("catalog-projection", "denied objects are duplicate or unsorted")
	}
	if err := validateCatalogExpressionClosure(body); err != nil {
		return err
	}
	return nil
}

func validateProjectedDeclaredCoverage(body CatalogProjectionBody) error {
	declared := make(map[string]ObjectIdentityProjection, len(body.DeclaredObjects))
	for _, identity := range body.DeclaredObjects {
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		declared[key] = identity
	}
	actual := make(map[string]struct{})
	requiredDeclared := make(map[string]struct{})
	addActual := func(identity ObjectIdentityProjection, required bool) error {
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		actual[key] = struct{}{}
		if required {
			requiredDeclared[key] = struct{}{}
		}
		return nil
	}
	if err := addActual(ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: body.Schema.Name}}, false); err != nil {
		return err
	}
	for _, relation := range body.Relations {
		if err := addActual(ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: relation.Identity}}, true); err != nil {
			return err
		}
		for _, column := range relation.Columns {
			if err := addActual(ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.Identity, Name: column.Name}}, false); err != nil {
				return err
			}
		}
		for _, constraint := range relation.Constraints {
			if err := addActual(ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.Identity, Name: constraint.Name}}, false); err != nil {
				return err
			}
		}
		for _, index := range relation.Indexes {
			constraintOwned := false
			for _, constraint := range relation.Constraints {
				if constraint.Name != index.Name {
					continue
				}
				constraintOwned = constraint.Type == "primary_key" && index.Primary || constraint.Type == "unique" && index.Unique && !index.Primary || constraint.Type == "exclusion" && index.Exclusion
				if constraintOwned {
					break
				}
			}
			if err := addActual(ObjectIdentityProjection{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: relation.Identity.Schema, Name: index.Name}, Relation: relation.Identity}}, !constraintOwned); err != nil {
				return err
			}
		}
		for _, policy := range relation.Policies {
			if err := addActual(ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.Identity, Name: policy.Name}}, true); err != nil {
				return err
			}
		}
		for _, trigger := range relation.Triggers {
			if trigger.Identity.Trigger != nil {
				if err := addActual(trigger.Identity, true); err != nil {
					return err
				}
			}
		}
	}
	for _, function := range body.Functions {
		if err := addActual(ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: function.Identity}}, true); err != nil {
			return err
		}
	}
	for key := range requiredDeclared {
		if _, ok := declared[key]; !ok {
			return invalidProjection("catalog-projection", "projected top-level object is outside declared_objects")
		}
	}
	for key, identity := range declared {
		switch identity.Kind() {
		case "schema", "relation", "column", "index", "policy", "function", "constraint", "trigger":
			if _, ok := actual[key]; !ok {
				return invalidProjection("catalog-projection", "declared object is absent from the catalog projection")
			}
		}
	}
	for _, denied := range body.DeniedObjects {
		key, err := canonicalContractKey(denied.Object)
		if err != nil {
			return err
		}
		if _, ok := declared[key]; ok {
			return invalidProjection("catalog-projection", "denied object overlaps declared_objects")
		}
	}
	return nil
}

func validateRelationProjections(relations []RelationProjection) error {
	keys := make([]string, len(relations))
	for relationIndex, relation := range relations {
		if err := relation.Identity.Validate(); err != nil || relation.Identity.Schema != projectionTargetSchema || relation.Owner == "" {
			return invalidProjection("catalog-projection", "relation identity or owner is invalid")
		}
		switch relation.Relkind {
		case "table", "partitioned_table", "view", "materialized_view", "foreign_table", "sequence":
		default:
			return invalidProjection("catalog-projection", "relation kind is outside the closed profile")
		}
		switch relation.Persistence {
		case "permanent", "unlogged", "temporary":
		default:
			return invalidProjection("catalog-projection", "relation persistence is outside the closed profile")
		}
		if relation.AccessMethod != nil && *relation.AccessMethod == "" {
			return invalidProjection("catalog-projection", "non-null relation access method is empty")
		}
		if relation.Relkind == "table" || relation.Relkind == "partitioned_table" || relation.Relkind == "materialized_view" {
			if relation.AccessMethod == nil {
				return invalidProjection("catalog-projection", "stored relation is missing its access method")
			}
		} else if relation.AccessMethod != nil {
			return invalidProjection("catalog-projection", "non-stored relation carries an access method")
		}
		if relation.ExplicitACL.Entries == nil || relation.Reloptions == nil || relation.Columns == nil || relation.Constraints == nil || relation.Indexes == nil || relation.Policies == nil || relation.Triggers == nil {
			return invalidProjection("catalog-projection", "relation projection is sparse")
		}
		if err := relation.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(relation.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		allowedPrivileges := []string{"DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"}
		if relation.Relkind == "sequence" {
			allowedPrivileges = []string{"SELECT", "UPDATE", "USAGE"}
		}
		if err := validateACLPrivileges(relation.ExplicitACL.Entries, allowedPrivileges...); err != nil {
			return err
		}
		if !strictlySorted(relation.Reloptions) && len(relation.Reloptions) > 1 {
			return invalidProjection("catalog-projection", "relation options are duplicate or unsorted")
		}
		switch relation.ReplicaIdentity {
		case "default", "nothing", "full", "index":
		default:
			return invalidProjection("catalog-projection", "relation replica identity is outside the closed profile")
		}
		if relation.RLSForced && !relation.RLSEnabled {
			return invalidProjection("catalog-projection", "forced row security requires row security to be enabled")
		}
		if err := validateColumnProjections(relation); err != nil {
			return err
		}
		if err := validateConstraintProjections(relation); err != nil {
			return err
		}
		if err := validateIndexProjections(relation); err != nil {
			return err
		}
		if err := validatePolicyProjections(relation); err != nil {
			return err
		}
		if err := validateTriggerProjections(relation); err != nil {
			return err
		}
		keys[relationIndex] = relation.Identity.Schema + "\x00" + relation.Identity.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "relations are duplicate or unsorted")
	}
	return nil
}

func validateColumnProjections(relation RelationProjection) error {
	var previousAttnum uint32
	names := make(map[string]struct{}, len(relation.Columns))
	for index, column := range relation.Columns {
		if column.Attnum == 0 || index > 0 && column.Attnum <= previousAttnum || column.Name == "" {
			return invalidProjection("catalog-projection", "columns are duplicate, unsorted, or incomplete")
		}
		if _, duplicate := names[column.Name]; duplicate {
			return invalidProjection("catalog-projection", "column identity is duplicate")
		}
		names[column.Name] = struct{}{}
		previousAttnum = column.Attnum
		if err := column.Type.Validate(); err != nil {
			return err
		}
		if _, err := ValidateSignedIntegerDecimal(column.TypmodInt32Decimal, 32); err != nil {
			return err
		}
		if column.Collation != nil {
			if err := column.Collation.Validate(); err != nil {
				return err
			}
		}
		switch column.Identity {
		case "none", "always", "by_default":
		default:
			return invalidProjection("catalog-projection", "column identity mode is outside the closed profile")
		}
		switch column.Generated {
		case "none", "stored":
		default:
			return invalidProjection("catalog-projection", "column generated mode is outside the PG15-PG17 profile")
		}
		if column.Generated == "stored" && column.Default == nil {
			return invalidProjection("catalog-projection", "generated column lacks its normalized expression")
		}
		if column.Default != nil {
			if err := validateExpressionNodeType(*column.Default, column.Type); err != nil {
				return err
			}
		}
		switch column.Storage {
		case "plain", "external", "extended", "main":
		default:
			return invalidProjection("catalog-projection", "column storage mode is outside the closed profile")
		}
		switch column.Compression {
		case "default", "pglz", "lz4":
		default:
			return invalidProjection("catalog-projection", "column compression is outside the closed profile")
		}
		if column.ExplicitACL.Entries == nil {
			return invalidProjection("catalog-projection", "column ACL is sparse")
		}
		if err := column.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(column.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		if err := validateACLPrivileges(column.ExplicitACL.Entries, "INSERT", "REFERENCES", "SELECT", "UPDATE"); err != nil {
			return err
		}
	}
	return nil
}

func validateConstraintProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Constraints))
	columnSet := make(map[string]struct{}, len(relation.Columns))
	for _, column := range relation.Columns {
		columnSet[column.Name] = struct{}{}
	}
	for index, constraint := range relation.Constraints {
		if constraint.Name == "" || constraint.Columns == nil || constraint.ReferencedColumns == nil {
			return invalidProjection("catalog-projection", "constraint is sparse")
		}
		switch constraint.Type {
		case "primary_key", "unique":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 || constraint.Expression != nil {
				return invalidProjection("catalog-projection", "key constraint carries foreign or expression state")
			}
		case "foreign_key":
			if constraint.ReferencedRelation == nil || len(constraint.Columns) == 0 || len(constraint.Columns) != len(constraint.ReferencedColumns) || constraint.Expression != nil {
				return invalidProjection("catalog-projection", "foreign key constraint is incomplete")
			}
			if err := constraint.ReferencedRelation.Validate(); err != nil {
				return err
			}
		case "check":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 || constraint.Expression == nil {
				return invalidProjection("catalog-projection", "check constraint lacks its normalized expression")
			}
			if err := validateExpressionNodeType(*constraint.Expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		case "exclusion":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 {
				return invalidProjection("catalog-projection", "exclusion constraint carries foreign-key state")
			}
			if constraint.Expression != nil {
				if err := validateExpressionNodeType(*constraint.Expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
					return err
				}
			}
		default:
			return invalidProjection("catalog-projection", "constraint type is outside the closed profile")
		}
		if err := validateDistinctColumnNames("constraint columns", constraint.Columns, columnSet); err != nil {
			return err
		}
		if err := validateDistinctNames("referenced constraint columns", constraint.ReferencedColumns); err != nil {
			return err
		}
		if constraint.Deferred && !constraint.Deferrable {
			return invalidProjection("catalog-projection", "initially deferred constraint is not deferrable")
		}
		if err := validateConstraintActionProfile(constraint); err != nil {
			return err
		}
		keys[index] = constraint.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "constraints are duplicate or unsorted")
	}
	return nil
}

func validateConstraintActionProfile(constraint ConstraintProjection) error {
	if constraint.Type != "foreign_key" {
		if constraint.Match != "none" || constraint.Update != "none" || constraint.Delete != "none" {
			return invalidProjection("catalog-projection", "non-foreign constraint carries referential actions")
		}
		return nil
	}
	switch constraint.Match {
	case "simple", "full", "partial":
	default:
		return invalidProjection("catalog-projection", "foreign key match type is outside the closed profile")
	}
	for _, action := range []string{constraint.Update, constraint.Delete} {
		switch action {
		case "no_action", "restrict", "cascade", "set_null", "set_default":
		default:
			return invalidProjection("catalog-projection", "foreign key action is outside the closed profile")
		}
	}
	return nil
}

func validateIndexProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Indexes))
	columnSet := make(map[string]struct{}, len(relation.Columns))
	for _, column := range relation.Columns {
		columnSet[column.Name] = struct{}{}
	}
	for index, projected := range relation.Indexes {
		if projected.Name == "" || projected.AccessMethod == "" || projected.Terms == nil || projected.Includes == nil {
			return invalidProjection("catalog-projection", "index projection is sparse")
		}
		if projected.Predicate != nil {
			if err := validateExpressionNodeType(*projected.Predicate, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		}
		for termIndex, term := range projected.Terms {
			if term.Ordinal != uint32(termIndex+1) || term.OpclassOptions == nil {
				return invalidProjection("catalog-projection", "index term ordinal or options are invalid")
			}
			switch term.TermKind {
			case "column":
				if term.Column == nil || term.Expression != nil {
					return invalidProjection("catalog-projection", "column index term is incomplete")
				}
				if _, ok := columnSet[*term.Column]; !ok {
					return invalidProjection("catalog-projection", "index term references an unknown column")
				}
			case "expression":
				if term.Column != nil || term.Expression == nil {
					return invalidProjection("catalog-projection", "expression index term is incomplete")
				}
				if err := validateExpressionNode(*term.Expression); err != nil {
					return err
				}
			default:
				return invalidProjection("catalog-projection", "index term kind is outside the closed profile")
			}
			if term.Opclass == nil {
				return invalidProjection("catalog-projection", "index term is missing its operator class")
			}
			if err := term.Opclass.Validate(); err != nil {
				return err
			}
			if !strictlySorted(term.OpclassOptions) && len(term.OpclassOptions) > 1 {
				return invalidProjection("catalog-projection", "index operator class options are duplicate or unsorted")
			}
			if term.Collation != nil {
				if err := term.Collation.Validate(); err != nil {
					return err
				}
			}
			switch term.Order {
			case "asc", "desc":
			default:
				return invalidProjection("catalog-projection", "index order is outside the closed profile")
			}
			switch term.Nulls {
			case "first", "last":
			default:
				return invalidProjection("catalog-projection", "index null ordering is outside the closed profile")
			}
			if term.ExclusionOperator != nil {
				if err := term.ExclusionOperator.Validate(); err != nil {
					return err
				}
			}
		}
		if err := validateDistinctColumnNames("index includes", projected.Includes, columnSet); err != nil {
			return err
		}
		keys[index] = projected.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "indexes are duplicate or unsorted")
	}
	return nil
}

func validatePolicyProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Policies))
	for index, policy := range relation.Policies {
		if policy.Name == "" || policy.Roles == nil {
			return invalidProjection("catalog-projection", "policy projection is sparse")
		}
		switch policy.Command {
		case "all", "select", "insert", "update", "delete":
		default:
			return invalidProjection("catalog-projection", "policy command is outside the closed profile")
		}
		if !strictlySorted(policy.Roles) || len(policy.Roles) == 0 {
			return invalidProjection("catalog-projection", "policy roles are empty, duplicate, or unsorted")
		}
		for _, expression := range []*ExpressionNode{policy.Using, policy.WithCheck} {
			if expression != nil {
				if err := validateExpressionNodeType(*expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
					return err
				}
			}
		}
		keys[index] = policy.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "policies are duplicate or unsorted")
	}
	return nil
}

func validateTriggerProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Triggers))
	for index, trigger := range relation.Triggers {
		if err := trigger.Identity.Validate(); err != nil || trigger.OwningRelation != relation.Identity {
			return invalidProjection("catalog-projection", "trigger identity or owning relation is invalid")
		}
		if err := trigger.Function.Validate(); err != nil {
			return err
		}
		if trigger.Columns == nil || trigger.Arguments == nil {
			return invalidProjection("catalog-projection", "trigger projection is sparse")
		}
		switch trigger.Enabled {
		case "origin", "always", "replica", "disabled":
		default:
			return invalidProjection("catalog-projection", "trigger enabled state is outside the closed profile")
		}
		if trigger.Type == 0 {
			return invalidProjection("catalog-projection", "trigger event type is empty")
		}
		if trigger.When != nil {
			if err := validateExpressionNodeType(*trigger.When, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		}
		if trigger.Internal != (trigger.Identity.Internal != nil) {
			return invalidProjection("catalog-projection", "trigger internal flag differs from its normalized identity")
		}
		key, err := canonicalContractKey(trigger.Identity)
		if err != nil {
			return err
		}
		keys[index] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "triggers are duplicate or unsorted")
	}
	return nil
}

func validateFunctionProjections(functions []FunctionProjection) error {
	keys := make([]string, len(functions))
	for functionIndex, function := range functions {
		if err := function.Identity.Validate(); err != nil || function.Identity.Schema != projectionTargetSchema || function.Language == "" || function.Owner == "" || function.Arguments == nil || function.Config == nil || function.ExplicitACL.Entries == nil {
			return invalidProjection("catalog-projection", "function projection is sparse or has invalid identity")
		}
		switch function.Kind {
		case "function", "procedure", "aggregate", "window":
		default:
			return invalidProjection("catalog-projection", "function kind is outside the closed profile")
		}
		if err := function.Returns.Validate(); err != nil {
			return err
		}
		if function.VariadicType != nil {
			if err := function.VariadicType.Validate(); err != nil {
				return err
			}
		}
		for argumentIndex, argument := range function.Arguments {
			if argument.Ordinal != uint32(argumentIndex+1) {
				return invalidProjection("catalog-projection", "function argument ordinals are not contiguous")
			}
			if argument.Name != nil && *argument.Name == "" {
				return invalidProjection("catalog-projection", "non-null function argument name is empty")
			}
			switch argument.Mode {
			case "in", "out", "inout", "variadic", "table":
			default:
				return invalidProjection("catalog-projection", "function argument mode is outside the closed profile")
			}
			if err := argument.Type.Validate(); err != nil {
				return err
			}
			if argument.Default != nil {
				if err := validateExpressionNodeType(*argument.Default, argument.Type); err != nil {
					return err
				}
			}
		}
		if err := function.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(function.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		if err := validateACLPrivileges(function.ExplicitACL.Entries, "EXECUTE"); err != nil {
			return err
		}
		switch function.Volatility {
		case "immutable", "stable", "volatile":
		default:
			return invalidProjection("catalog-projection", "function volatility is outside the closed profile")
		}
		switch function.Parallel {
		case "safe", "restricted", "unsafe":
		default:
			return invalidProjection("catalog-projection", "function parallel mode is outside the closed profile")
		}
		if !strictlySorted(function.Config) && len(function.Config) > 1 {
			return invalidProjection("catalog-projection", "function config is duplicate or unsorted")
		}
		if err := ValidateExactNumeric(function.Cost); err != nil {
			return err
		}
		if err := ValidateExactNumeric(function.Rows); err != nil {
			return err
		}
		if err := requireDigest("catalog-projection.function.prosrc_sha256", function.ProsrcSHA256); err != nil {
			return err
		}
		if function.Probin != nil && *function.Probin == "" {
			return invalidProjection("catalog-projection", "non-null function probin is empty")
		}
		key, err := canonicalContractKey(function.Identity)
		if err != nil {
			return err
		}
		keys[functionIndex] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "functions are duplicate or unsorted")
	}
	return nil
}

func validateDistinctColumnNames(path string, values []string, available map[string]struct{}) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return invalidProjection("catalog-projection", path+" contain an empty name")
		}
		if _, duplicate := seen[value]; duplicate {
			return invalidProjection("catalog-projection", path+" are duplicate")
		}
		if _, ok := available[value]; !ok {
			return invalidProjection("catalog-projection", path+" reference an unknown column")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateDistinctNames(path string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return invalidProjection("catalog-projection", path+" contain an empty name")
		}
		if _, duplicate := seen[value]; duplicate {
			return invalidProjection("catalog-projection", path+" are duplicate")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func ValidateSignedIntegerDecimal(value string, bits int) (int64, error) {
	if !signedIntegerDecimalPattern.MatchString(value) {
		return 0, invalidProjection("numeric-profile", "signed integer is not canonical decimal")
	}
	if bits != 16 && bits != 32 && bits != 64 {
		return 0, invalidProjection("numeric-profile", "unsupported signed integer width")
	}
	parsed, err := strconv.ParseInt(value, 10, bits)
	if err != nil {
		return 0, invalidProjection("numeric-profile", fmt.Sprintf("signed integer exceeds int%d", bits))
	}
	return parsed, nil
}

func ValidateExactNumeric(value string) error {
	if len(value) == 0 || len(value) > 128 || value == "-0" || !exactNumericPattern.MatchString(value) {
		return invalidProjection("numeric-profile", "numeric is not canonical exact decimal")
	}
	return nil
}

// CanonicalExactNumeric accepts PostgreSQL plain-decimal output and returns the
// ADR-0010 exact numeric spelling. Exponents, a leading plus, and negative zero
// remain forbidden; positive zero fractions collapse to "0".
func CanonicalExactNumeric(value string) (string, error) {
	if len(value) == 0 || len(value) > 128 || strings.ContainsAny(value, "eE+") {
		return "", invalidProjection("numeric-profile", "numeric is outside the exact decimal input profile")
	}
	negative := strings.HasPrefix(value, "-")
	unsigned := strings.TrimPrefix(value, "-")
	integer, fraction, hasFraction := strings.Cut(unsigned, ".")
	if integer == "" || (len(integer) > 1 && integer[0] == '0') {
		return "", invalidProjection("numeric-profile", "numeric integer part is not canonical")
	}
	for _, digit := range integer + fraction {
		if digit < '0' || digit > '9' {
			return "", invalidProjection("numeric-profile", "numeric contains a non-decimal digit")
		}
	}
	if hasFraction && fraction == "" {
		return "", invalidProjection("numeric-profile", "numeric fraction is empty")
	}
	fraction = strings.TrimRight(fraction, "0")
	zero := integer == "0" && fraction == ""
	if negative && zero {
		return "", invalidProjection("numeric-profile", "negative zero is forbidden")
	}
	canonical := integer
	if fraction != "" {
		canonical += "." + fraction
	}
	if negative {
		canonical = "-" + canonical
	}
	if err := ValidateExactNumeric(canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

func ValidateRyuFloat32(value string) error { return validateRyuFloat(value, 32) }
func ValidateRyuFloat64(value string) error { return validateRyuFloat(value, 64) }

func validateRyuFloat(value string, bits int) error {
	if len(value) == 0 || len(value) > 32 || !ryuDecimalPattern.MatchString(value) {
		return invalidProjection("numeric-profile", "float is outside cloud-agents-ryu-v1 lexical profile")
	}
	parsed, err := strconv.ParseFloat(value, bits)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return invalidProjection("numeric-profile", "float is not finite in the requested PostgreSQL width")
	}
	if parsed == 0 && math.Signbit(parsed) {
		return invalidProjection("numeric-profile", "negative zero is forbidden")
	}
	canonical := normalizeRyuExponent(strconv.FormatFloat(parsed, 'g', -1, bits))
	if canonical != value {
		return invalidProjection("numeric-profile", "float is not the shortest canonical round-trip decimal")
	}
	return nil
}

func normalizeRyuExponent(value string) string {
	marker := strings.IndexByte(value, 'e')
	if marker < 0 {
		return value
	}
	mantissa, exponent := value[:marker], value[marker+1:]
	negative := strings.HasPrefix(exponent, "-")
	exponent = strings.TrimPrefix(strings.TrimPrefix(exponent, "+"), "-")
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}
	if negative {
		exponent = "-" + exponent
	}
	return mantissa + "e" + exponent
}

func (state CatalogStateProjection) ComputeDigest() (Digest, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	return digestFlatDomain(CatalogStateDigestDomain, state, "")
}

func digestFlatDomain(domain string, value any, excludedField string) (Digest, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", invalidProjection("projection-digest", "cannot encode typed projection")
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return "", err
	}
	object, ok := parsed.(map[string]JSONValue)
	if !ok {
		return "", invalidProjection("projection-digest", "digest input is not an object")
	}
	if excludedField != "" {
		delete(object, excludedField)
	}
	object["domain"] = domain
	canonical, err := CanonicalJSON(object)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

func canonicalContractKey(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", invalidProjection("projection-contract", "cannot encode typed key")
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalJSON(parsed)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func strictlySorted(values []string) bool {
	return sort.SliceIsSorted(values, func(i, j int) bool { return values[i] < values[j] }) && noDuplicateStrings(values)
}

func noDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func invalidProjection(op, message string) error {
	return fail(CodeInvalidManifest, op, message, nil)
}

func equalObjectIdentityClosures(left, right []ObjectIdentityProjection) bool {
	if len(left) != len(right) || (left == nil) != (right == nil) {
		return false
	}
	for index := range left {
		leftKey, leftErr := canonicalContractKey(left[index])
		rightKey, rightErr := canonicalContractKey(right[index])
		if leftErr != nil || rightErr != nil || leftKey != rightKey {
			return false
		}
	}
	return true
}

const (
	projectionMaxPrincipals uint64 = 256

	projectionMaxExpressionNodes uint64 = 4_096
)

const (
	projectionTargetSchema = "cloud_agents"
)
