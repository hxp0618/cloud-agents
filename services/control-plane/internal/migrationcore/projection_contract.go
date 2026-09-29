package migrationcore

import "encoding/json"

const (
	CatalogStateDigestDomain = "cloud-agents-platform-catalog-state/v1"
)

type TypeIdentity struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

type SQLIdentity struct {
	Schema    string         `json:"schema"`
	Name      string         `json:"name"`
	Arguments []TypeIdentity `json:"arguments"`
}

type SchemaObjectIdentity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type RelationObjectIdentity struct {
	Kind     string       `json:"kind"`
	Identity TypeIdentity `json:"identity"`
}

type ColumnObjectIdentity struct {
	Kind     string       `json:"kind"`
	Relation TypeIdentity `json:"relation"`
	Name     string       `json:"name"`
}

type IndexObjectIdentity struct {
	Kind     string       `json:"kind"`
	Identity TypeIdentity `json:"identity"`
	Relation TypeIdentity `json:"relation"`
}

type PolicyObjectIdentity struct {
	Kind     string       `json:"kind"`
	Relation TypeIdentity `json:"relation"`
	Name     string       `json:"name"`
}

type TypeObjectIdentity struct {
	Kind     string       `json:"kind"`
	Identity TypeIdentity `json:"identity"`
}

type ExtensionObjectIdentity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type CollationObjectIdentity struct {
	Kind     string       `json:"kind"`
	Identity TypeIdentity `json:"identity"`
}

type OpclassObjectIdentity struct {
	Kind         string       `json:"kind"`
	Identity     TypeIdentity `json:"identity"`
	AccessMethod string       `json:"access_method"`
}

type SQLObjectIdentity struct {
	Kind     string      `json:"kind"`
	Identity SQLIdentity `json:"identity"`
}

type CastObjectIdentity struct {
	Kind       string       `json:"kind"`
	SourceType TypeIdentity `json:"source_type"`
	TargetType TypeIdentity `json:"target_type"`
}

type ConstraintObjectIdentity struct {
	Kind     string       `json:"kind"`
	Relation TypeIdentity `json:"relation"`
	Name     string       `json:"name"`
}

type TriggerObjectIdentity struct {
	Kind             string                    `json:"kind"`
	Relation         TypeIdentity              `json:"relation"`
	Name             string                    `json:"name"`
	OwningConstraint *ConstraintObjectIdentity `json:"owning_constraint"`
}

type InternalObjectIdentity struct {
	Kind         string                   `json:"kind"`
	SemanticKind string                   `json:"semantic_kind"`
	OwningObject ObjectIdentityProjection `json:"owning_object"`
}

// ObjectIdentityProjection is a closed tagged union. Exactly one branch is set.
type ObjectIdentityProjection struct {
	Schema     *SchemaObjectIdentity
	Relation   *RelationObjectIdentity
	Column     *ColumnObjectIdentity
	Index      *IndexObjectIdentity
	Policy     *PolicyObjectIdentity
	Type       *TypeObjectIdentity
	Extension  *ExtensionObjectIdentity
	Collation  *CollationObjectIdentity
	Opclass    *OpclassObjectIdentity
	Function   *SQLObjectIdentity
	Operator   *SQLObjectIdentity
	Cast       *CastObjectIdentity
	Constraint *ConstraintObjectIdentity
	Trigger    *TriggerObjectIdentity
	Internal   *InternalObjectIdentity
}

func (identity *ObjectIdentityProjection) UnmarshalJSON(data []byte) error {
	value, err := ParseStrictJSON(data)
	if err != nil {
		return err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return fail(CodeInvalidJSON, "object-identity", "object identity must be an object", nil)
	}
	discriminator, ok := object["kind"].(string)
	if !ok {
		return fail(CodeInvalidJSON, "object-identity", "object identity kind is missing", nil)
	}
	*identity = ObjectIdentityProjection{}
	switch discriminator {
	case "schema":
		identity.Schema = &SchemaObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Schema)
		return err
	case "relation":
		identity.Relation = &RelationObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Relation)
		return err
	case "column":
		identity.Column = &ColumnObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Column)
		return err
	case "index":
		identity.Index = &IndexObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Index)
		return err
	case "policy":
		identity.Policy = &PolicyObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Policy)
		return err
	case "type":
		identity.Type = &TypeObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Type)
		return err
	case "extension":
		identity.Extension = &ExtensionObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Extension)
		return err
	case "collation":
		identity.Collation = &CollationObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Collation)
		return err
	case "opclass":
		identity.Opclass = &OpclassObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Opclass)
		return err
	case "function":
		identity.Function = &SQLObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Function)
		return err
	case "operator":
		identity.Operator = &SQLObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Operator)
		return err
	case "cast":
		identity.Cast = &CastObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Cast)
		return err
	case "constraint":
		identity.Constraint = &ConstraintObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Constraint)
		return err
	case "trigger":
		identity.Trigger = &TriggerObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Trigger)
		return err
	case "internal":
		identity.Internal = &InternalObjectIdentity{}
		_, err := decodeStrictShape(data, identity.Internal)
		return err
	default:
		return fail(CodeInvalidManifest, "object-identity", "unknown object identity kind", nil)
	}
}

func (identity ObjectIdentityProjection) MarshalJSON() ([]byte, error) {
	branch, err := identity.branch()
	if err != nil {
		return nil, err
	}
	return json.Marshal(branch)
}

type ACLProjection struct {
	Grantor    string   `json:"grantor"`
	Grantee    string   `json:"grantee"`
	Privileges []string `json:"privileges"`
	Grantable  []string `json:"grantable"`
	Origin     string   `json:"origin"`
}

type ACLSetProjection struct {
	CatalogValue string          `json:"catalog_value"`
	Entries      []ACLProjection `json:"entries"`
}

type SecurityLabel struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

type SchemaProjection struct {
	Name           string           `json:"name"`
	Owner          string           `json:"owner"`
	ExplicitACL    ACLSetProjection `json:"explicit_acl"`
	EffectiveACL   []ACLProjection  `json:"effective_acl"`
	Comment        *string          `json:"comment"`
	SecurityLabels []SecurityLabel  `json:"security_labels"`
}

type DefaultACLProjection struct {
	Owner      string           `json:"owner"`
	Schema     *string          `json:"schema"`
	ObjectKind string           `json:"object_kind"`
	ACL        ACLSetProjection `json:"acl"`
}

type DependencyProjection struct {
	Depender       ObjectIdentityProjection `json:"depender"`
	DependedOn     ObjectIdentityProjection `json:"depended_on"`
	DependencyKind string                   `json:"dependency_kind"`
}

type DeniedObjectProjection struct {
	Object         ObjectIdentityProjection  `json:"object"`
	Owner          *string                   `json:"owner"`
	DependencyKind *string                   `json:"dependency_kind"`
	DependedOn     *ObjectIdentityProjection `json:"depended_on"`
	ReasonCode     string                    `json:"reason_code"`
}

type ExpressionNode struct {
	Kind     string            `json:"kind"`
	Type     *TypeIdentity     `json:"type"`
	Identity *SQLIdentity      `json:"identity"`
	Value    JSONValue         `json:"value"`
	Fields   map[string]string `json:"fields"`
	Children []ExpressionNode  `json:"children"`
}

type ColumnProjection struct {
	Attnum             uint32           `json:"attnum"`
	Name               string           `json:"name"`
	Type               TypeIdentity     `json:"type"`
	TypmodInt32Decimal string           `json:"typmod_int32_decimal"`
	Collation          *TypeIdentity    `json:"collation"`
	Nullable           bool             `json:"nullable"`
	Identity           string           `json:"identity"`
	Generated          string           `json:"generated"`
	Default            *ExpressionNode  `json:"default"`
	Storage            string           `json:"storage"`
	Compression        string           `json:"compression"`
	ExplicitACL        ACLSetProjection `json:"explicit_acl"`
}

type ConstraintProjection struct {
	Name               string          `json:"name"`
	Type               string          `json:"type"`
	Columns            []string        `json:"columns"`
	ReferencedRelation *TypeIdentity   `json:"referenced_relation"`
	ReferencedColumns  []string        `json:"referenced_columns"`
	Match              string          `json:"match"`
	Update             string          `json:"update"`
	Delete             string          `json:"delete"`
	Deferrable         bool            `json:"deferrable"`
	Deferred           bool            `json:"deferred"`
	Validated          bool            `json:"validated"`
	Expression         *ExpressionNode `json:"expression"`
}

type IndexTermProjection struct {
	Ordinal           uint32          `json:"ordinal"`
	TermKind          string          `json:"term_kind"`
	Column            *string         `json:"column"`
	Expression        *ExpressionNode `json:"expression"`
	Opclass           *TypeIdentity   `json:"opclass"`
	OpclassOptions    []string        `json:"opclass_options"`
	Collation         *TypeIdentity   `json:"collation"`
	Order             string          `json:"order"`
	Nulls             string          `json:"nulls"`
	ExclusionOperator *SQLIdentity    `json:"exclusion_operator"`
}

type IndexProjection struct {
	Name             string                `json:"name"`
	AccessMethod     string                `json:"access_method"`
	Terms            []IndexTermProjection `json:"terms"`
	Includes         []string              `json:"includes"`
	Unique           bool                  `json:"unique"`
	Primary          bool                  `json:"primary"`
	Valid            bool                  `json:"valid"`
	Ready            bool                  `json:"ready"`
	Live             bool                  `json:"live"`
	Immediate        bool                  `json:"immediate"`
	Clustered        bool                  `json:"clustered"`
	CheckXmin        bool                  `json:"check_xmin"`
	NullsNotDistinct bool                  `json:"nulls_not_distinct"`
	Exclusion        bool                  `json:"exclusion"`
	ReplicaIdentity  bool                  `json:"replica_identity"`
	Predicate        *ExpressionNode       `json:"predicate"`
}

type PolicyProjection struct {
	Name       string          `json:"name"`
	Permissive bool            `json:"permissive"`
	Command    string          `json:"command"`
	Roles      []string        `json:"roles"`
	Using      *ExpressionNode `json:"using"`
	WithCheck  *ExpressionNode `json:"with_check"`
}

type TriggerProjection struct {
	Identity         ObjectIdentityProjection  `json:"identity"`
	OwningRelation   TypeIdentity              `json:"owning_relation"`
	OwningConstraint *ConstraintObjectIdentity `json:"owning_constraint"`
	Function         SQLIdentity               `json:"function"`
	Enabled          string                    `json:"enabled"`
	Type             uint32                    `json:"type"`
	Columns          []string                  `json:"columns"`
	Arguments        []string                  `json:"arguments"`
	When             *ExpressionNode           `json:"when"`
	Internal         bool                      `json:"internal"`
}

type FunctionArgumentProjection struct {
	Ordinal uint32          `json:"ordinal"`
	Name    *string         `json:"name"`
	Mode    string          `json:"mode"`
	Type    TypeIdentity    `json:"type"`
	Default *ExpressionNode `json:"default"`
}

type FunctionProjection struct {
	Identity        SQLIdentity                  `json:"identity"`
	Kind            string                       `json:"kind"`
	Language        string                       `json:"language"`
	Arguments       []FunctionArgumentProjection `json:"arguments"`
	VariadicType    *TypeIdentity                `json:"variadic_type"`
	Returns         TypeIdentity                 `json:"returns"`
	ReturnSet       bool                         `json:"return_set"`
	Owner           string                       `json:"owner"`
	ExplicitACL     ACLSetProjection             `json:"explicit_acl"`
	SecurityDefiner bool                         `json:"security_definer"`
	Volatility      string                       `json:"volatility"`
	Parallel        string                       `json:"parallel"`
	Leakproof       bool                         `json:"leakproof"`
	Strict          bool                         `json:"strict"`
	Config          []string                     `json:"config"`
	Cost            string                       `json:"cost"`
	Rows            string                       `json:"rows"`
	ProsrcSHA256    Digest                       `json:"prosrc_sha256"`
	Probin          *string                      `json:"probin"`
}

type RelationProjection struct {
	Identity        TypeIdentity           `json:"identity"`
	Relkind         string                 `json:"relkind"`
	Persistence     string                 `json:"persistence"`
	AccessMethod    *string                `json:"access_method"`
	Owner           string                 `json:"owner"`
	ExplicitACL     ACLSetProjection       `json:"explicit_acl"`
	Reloptions      []string               `json:"reloptions"`
	ReplicaIdentity string                 `json:"replica_identity"`
	RLSEnabled      bool                   `json:"rls_enabled"`
	RLSForced       bool                   `json:"rls_forced"`
	Columns         []ColumnProjection     `json:"columns"`
	Constraints     []ConstraintProjection `json:"constraints"`
	Indexes         []IndexProjection      `json:"indexes"`
	Policies        []PolicyProjection     `json:"policies"`
	Triggers        []TriggerProjection    `json:"triggers"`
}

type CatalogProjectionBody struct {
	Schema          SchemaProjection           `json:"schema"`
	DefaultACL      []DefaultACLProjection     `json:"default_acl"`
	Relations       []RelationProjection       `json:"relations"`
	Functions       []FunctionProjection       `json:"functions"`
	Dependencies    []DependencyProjection     `json:"dependencies"`
	ObjectCount     uint32                     `json:"object_count"`
	DeclaredObjects []ObjectIdentityProjection `json:"declared_objects"`
	DeniedObjects   []DeniedObjectProjection   `json:"denied_objects"`
}

type ProjectionScope struct {
	ScopeKind             string                     `json:"scope_kind"`
	SchemaHead            *string                    `json:"schema_head"`
	MigrationID           *string                    `json:"migration_id"`
	ThroughStatementIndex *uint32                    `json:"through_statement_index"`
	DeclaredObjects       []ObjectIdentityProjection `json:"declared_objects"`
}

type SchemaAbsentProjection struct {
	State  string          `json:"state"`
	Scope  ProjectionScope `json:"scope"`
	Schema string          `json:"schema"`
}

type SchemaPresentProjection struct {
	State string                `json:"state"`
	Scope ProjectionScope       `json:"scope"`
	Body  CatalogProjectionBody `json:"body"`
}

type CatalogStateProjection struct {
	Absent  *SchemaAbsentProjection
	Present *SchemaPresentProjection
}

func (state *CatalogStateProjection) UnmarshalJSON(data []byte) error {
	value, err := ParseStrictJSON(data)
	if err != nil {
		return err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return fail(CodeInvalidJSON, "catalog-state", "catalog state must be an object", nil)
	}
	discriminator, ok := object["state"].(string)
	if !ok {
		return fail(CodeInvalidJSON, "catalog-state", "catalog state discriminator is missing", nil)
	}
	*state = CatalogStateProjection{}
	switch discriminator {
	case "schema_absent":
		state.Absent = &SchemaAbsentProjection{}
		_, err := decodeStrictShape(data, state.Absent)
		return err
	case "schema_present":
		state.Present = &SchemaPresentProjection{}
		_, err := decodeStrictShape(data, state.Present)
		return err
	default:
		return fail(CodeInvalidManifest, "catalog-state", "unknown catalog state discriminator", nil)
	}
}

func (state CatalogStateProjection) MarshalJSON() ([]byte, error) {
	switch {
	case state.Absent != nil && state.Present == nil:
		return json.Marshal(state.Absent)
	case state.Absent == nil && state.Present != nil:
		return json.Marshal(state.Present)
	default:
		return nil, fail(CodeInvalidManifest, "catalog-state", "catalog state must contain exactly one branch", nil)
	}
}
