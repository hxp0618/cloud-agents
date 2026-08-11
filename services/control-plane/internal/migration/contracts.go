package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

type SQLClassificationDescriptor struct {
	Profile        string  `json:"profile"`
	Command        string  `json:"command"`
	ObjectKind     string  `json:"object_kind"`
	TargetIdentity string  `json:"target_identity"`
	Grantee        *string `json:"grantee"`
	SpecialCase    *string `json:"special_case"`
}

type SQLStatementDescriptor struct {
	Index              uint64                      `json:"index"`
	Start              uint64                      `json:"start"`
	End                uint64                      `json:"end"`
	SHA256             Digest                      `json:"sha256"`
	Classification     SQLClassificationDescriptor `json:"classification"`
	ExpectedTransition ExpectedStatementTransition `json:"expected_transition"`
}

type SQLSourceDescriptor struct {
	MigrationID string                   `json:"migration_id"`
	SQLSHA256   Digest                   `json:"sql_sha256"`
	Statements  []SQLStatementDescriptor `json:"statements"`
}

type CatalogProjectionModel struct {
	SchemaFields      []string `json:"schema_fields"`
	DefaultACLFields  []string `json:"default_acl_fields"`
	RelationFields    []string `json:"relation_fields"`
	ColumnFields      []string `json:"column_fields"`
	ConstraintFields  []string `json:"constraint_fields"`
	IndexFields       []string `json:"index_fields"`
	PolicyFields      []string `json:"policy_fields"`
	TriggerFields     []string `json:"trigger_fields"`
	FunctionFields    []string `json:"function_fields"`
	ExpressionProfile string   `json:"expression_profile"`
	DeniedObjectKinds []string `json:"denied_object_kinds"`
}

type CatalogContract struct {
	FormatVersion              string                     `json:"format_version"`
	ContractKind               string                     `json:"contract_kind"`
	SchemaHead                 string                     `json:"schema_head"`
	PublicationStatus          string                     `json:"publication_status"`
	RuntimeIntrospectionStatus string                     `json:"runtime_introspection_status"`
	SourceDescriptors          []SQLSourceDescriptor      `json:"source_descriptors"`
	ProjectionModel            CatalogProjectionModel     `json:"projection_model"`
	DeclaredObjectIdentities   []ObjectIdentityProjection `json:"declared_object_identities"`
	ExpectedProjection         CatalogProjection          `json:"expected_projection"`
	bootstrapShape             bool
	bootstrapExecutableStatus  string
	bootstrapProjectionModel   bootstrapCatalogProjectionModel
}

func (contract CatalogContract) MarshalJSON() ([]byte, error) {
	if !contract.bootstrapShape {
		type plainCatalogContract CatalogContract
		return json.Marshal(plainCatalogContract(contract))
	}
	bootstrap := bootstrapCatalogContract{
		FormatVersion: contract.FormatVersion, ContractKind: contract.ContractKind,
		SchemaHead: contract.SchemaHead, PublicationStatus: contract.PublicationStatus,
		RuntimeIntrospectionStatus:         contract.RuntimeIntrospectionStatus,
		ProjectionModel:                    contract.bootstrapProjectionModel,
		DeclaredObjectIdentities:           cloneObjectIdentities(contract.DeclaredObjectIdentities),
		ExecutableExpectedProjectionStatus: contract.bootstrapExecutableStatus,
		SourceDescriptors:                  make([]bootstrapSQLSourceDescriptor, len(contract.SourceDescriptors)),
	}
	for sourceIndex, source := range contract.SourceDescriptors {
		bootstrap.SourceDescriptors[sourceIndex] = bootstrapSQLSourceDescriptor{MigrationID: source.MigrationID, SQLSHA256: source.SQLSHA256, Statements: make([]bootstrapSQLStatementDescriptor, len(source.Statements))}
		for statementIndex, statement := range source.Statements {
			bootstrap.SourceDescriptors[sourceIndex].Statements[statementIndex] = bootstrapSQLStatementDescriptor{Index: statement.Index, Start: statement.Start, End: statement.End, SHA256: statement.SHA256, Classification: statement.Classification}
		}
	}
	return json.Marshal(bootstrap)
}

// bootstrapCatalogContract is the only compatibility shape retained for the
// checked-in NOT_IMPLEMENTED/UNPUBLISHED artifacts. It cannot become an
// executable projection contract because it has no typed expected authority.
type bootstrapCatalogContract struct {
	FormatVersion                      string                          `json:"format_version"`
	ContractKind                       string                          `json:"contract_kind"`
	SchemaHead                         string                          `json:"schema_head"`
	PublicationStatus                  string                          `json:"publication_status"`
	RuntimeIntrospectionStatus         string                          `json:"runtime_introspection_status"`
	SourceDescriptors                  []bootstrapSQLSourceDescriptor  `json:"source_descriptors"`
	ProjectionModel                    bootstrapCatalogProjectionModel `json:"projection_model"`
	DeclaredObjectIdentities           []ObjectIdentityProjection      `json:"declared_object_identities"`
	ExecutableExpectedProjectionStatus string                          `json:"executable_expected_projection_status"`
}

type bootstrapCatalogProjectionModel struct {
	ProjectionSlice         string   `json:"projection_slice"`
	CatalogProjectionFields []string `json:"catalog_projection_fields"`
	BodyFields              []string `json:"body_fields"`
	SchemaFields            []string `json:"schema_fields"`
	DefaultACLFields        []string `json:"default_acl_fields"`
	ACLSetFields            []string `json:"acl_set_fields"`
	ACLEntryFields          []string `json:"acl_entry_fields"`
	DeferredToA21b          []string `json:"deferred_to_a2_1b"`
}

type bootstrapSQLSourceDescriptor struct {
	MigrationID string                            `json:"migration_id"`
	SQLSHA256   Digest                            `json:"sql_sha256"`
	Statements  []bootstrapSQLStatementDescriptor `json:"statements"`
}

type bootstrapSQLStatementDescriptor struct {
	Index          uint64                      `json:"index"`
	Start          uint64                      `json:"start"`
	End            uint64                      `json:"end"`
	SHA256         Digest                      `json:"sha256"`
	Classification SQLClassificationDescriptor `json:"classification"`
}

type AuthorityDatabaseContract struct {
	Encoding         string  `json:"encoding"`
	LocaleProvider   string  `json:"locale_provider"`
	Datcollate       string  `json:"datcollate"`
	Datctype         string  `json:"datctype"`
	ICULocale        *string `json:"icu_locale"`
	ICURules         *string `json:"icu_rules"`
	CollationVersion *string `json:"collation_version"`
}

type AuthorityContract struct {
	FormatVersion              string                    `json:"format_version"`
	ContractKind               string                    `json:"contract_kind"`
	PublicationStatus          string                    `json:"publication_status"`
	RuntimeIntrospectionStatus string                    `json:"runtime_introspection_status"`
	Database                   AuthorityDatabaseContract `json:"database"`
	GroupRoles                 []string                  `json:"group_roles"`
	RequiredProjectionFields   []string                  `json:"required_projection_fields"`
	RequiredBindingFields      []string                  `json:"required_binding_fields"`
}

type bootstrapAuthorityContract struct {
	FormatVersion              string                    `json:"format_version"`
	ContractKind               string                    `json:"contract_kind"`
	PublicationStatus          string                    `json:"publication_status"`
	RuntimeIntrospectionStatus string                    `json:"runtime_introspection_status"`
	Database                   AuthorityDatabaseContract `json:"database"`
	GroupRoles                 []string                  `json:"group_roles"`
	RequiredProjectionFields   []string                  `json:"required_projection_fields"`
}

type GlobalTableWriter struct {
	Name    string   `json:"name"`
	Writers []string `json:"writers"`
}

type GlobalTableAuthorityContract struct {
	FormatVersion              string              `json:"format_version"`
	ContractKind               string              `json:"contract_kind"`
	PublicationStatus          string              `json:"publication_status"`
	RuntimeIntrospectionStatus string              `json:"runtime_introspection_status"`
	Tables                     []GlobalTableWriter `json:"tables"`
}

func DecodeCatalogContract(data []byte) (*CatalogContract, error) {
	var contract CatalogContract
	value, err := ParseStrictJSON(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return nil, fail(CodeInvalidJSON, "catalog-contract", "catalog contract must be an object", nil)
	}
	publication, _ := object["publication_status"].(string)
	introspection, _ := object["runtime_introspection_status"].(string)
	_, hasExpectedProjection := object["expected_projection"]
	fullShape := hasExpectedProjection || publication != "UNPUBLISHED_BOOTSTRAP_MUTABLE" || introspection != "NOT_IMPLEMENTED"
	if fullShape {
		if _, err := DecodeStrict(data, &contract); err != nil {
			return nil, err
		}
	} else {
		var bootstrap bootstrapCatalogContract
		if _, err := DecodeStrict(data, &bootstrap); err != nil {
			return nil, err
		}
		contract = CatalogContract{
			FormatVersion: bootstrap.FormatVersion, ContractKind: bootstrap.ContractKind,
			SchemaHead: bootstrap.SchemaHead, PublicationStatus: bootstrap.PublicationStatus,
			RuntimeIntrospectionStatus: bootstrap.RuntimeIntrospectionStatus,
			bootstrapProjectionModel:   bootstrap.ProjectionModel,
			DeclaredObjectIdentities:   cloneObjectIdentities(bootstrap.DeclaredObjectIdentities),
			bootstrapShape:             true,
			bootstrapExecutableStatus:  bootstrap.ExecutableExpectedProjectionStatus,
		}
		contract.SourceDescriptors = make([]SQLSourceDescriptor, len(bootstrap.SourceDescriptors))
		for sourceIndex, source := range bootstrap.SourceDescriptors {
			contract.SourceDescriptors[sourceIndex] = SQLSourceDescriptor{MigrationID: source.MigrationID, SQLSHA256: source.SQLSHA256, Statements: make([]SQLStatementDescriptor, len(source.Statements))}
			for statementIndex, statement := range source.Statements {
				contract.SourceDescriptors[sourceIndex].Statements[statementIndex] = SQLStatementDescriptor{Index: statement.Index, Start: statement.Start, End: statement.End, SHA256: statement.SHA256, Classification: statement.Classification}
			}
		}
	}
	if contract.FormatVersion != "cloud-agents-platform-catalog/v1" || contract.ContractKind != "cumulative_schema_catalog" || !migrationIDPattern.MatchString(contract.SchemaHead) {
		return nil, fail(CodeInvalidManifest, "catalog-contract", "invalid catalog contract identity", nil)
	}
	for _, source := range contract.SourceDescriptors {
		if !migrationIDPattern.MatchString(source.MigrationID) {
			return nil, fail(CodeInvalidManifest, "catalog-contract", "source descriptor migration ID is invalid", nil)
		}
		if err := requireDigest("catalog.source.sql_sha256", source.SQLSHA256); err != nil {
			return nil, err
		}
		for index, statement := range source.Statements {
			if statement.Index != uint64(index) || statement.End <= statement.Start {
				return nil, fail(CodeInvalidManifest, "catalog-contract", "statement offsets or index are invalid", nil)
			}
			if err := requireDigest("catalog.statement.sha256", statement.SHA256); err != nil {
				return nil, err
			}
			if fullShape {
				if err := statement.ExpectedTransition.Validate(); err != nil {
					return nil, err
				}
			}
		}
	}
	if !fullShape {
		if contract.bootstrapExecutableStatus != "NOT_IMPLEMENTED_A2_1B_REQUIRED" {
			return nil, fail(CodeInvalidManifest, "catalog-contract", "bootstrap catalog must declare the A2.1b executable projection boundary", nil)
		}
		if err := contract.bootstrapProjectionModel.Validate(); err != nil {
			return nil, err
		}
		if err := validateObjectIdentityClosure(contract.DeclaredObjectIdentities); err != nil {
			return nil, err
		}
	}
	if fullShape {
		if err := validateObjectIdentityClosure(contract.DeclaredObjectIdentities); err != nil {
			return nil, err
		}
		if contract.ExpectedProjection.SchemaHead != contract.SchemaHead {
			return nil, fail(CodeInvalidManifest, "catalog-contract", "expected projection head differs from catalog head", nil)
		}
		if err := contract.ExpectedProjection.Validate(); err != nil {
			return nil, err
		}
		if err := validateExecutableCatalogBindings(contract); err != nil {
			return nil, err
		}
	}
	return &contract, nil
}

func DecodeAuthorityContract(data []byte) (*AuthorityContract, error) {
	var contract AuthorityContract
	value, err := ParseStrictJSON(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return nil, fail(CodeInvalidJSON, "authority-contract", "authority contract must be an object", nil)
	}
	publication, _ := object["publication_status"].(string)
	introspection, _ := object["runtime_introspection_status"].(string)
	_, hasBindingFields := object["required_binding_fields"]
	fullShape := hasBindingFields || publication != "UNPUBLISHED_BOOTSTRAP_MUTABLE" || introspection != "NOT_IMPLEMENTED"
	if fullShape {
		if _, err := DecodeStrict(data, &contract); err != nil {
			return nil, err
		}
	} else {
		var bootstrap bootstrapAuthorityContract
		if _, err := DecodeStrict(data, &bootstrap); err != nil {
			return nil, err
		}
		contract = AuthorityContract{
			FormatVersion: bootstrap.FormatVersion, ContractKind: bootstrap.ContractKind,
			PublicationStatus: bootstrap.PublicationStatus, RuntimeIntrospectionStatus: bootstrap.RuntimeIntrospectionStatus,
			Database: bootstrap.Database, GroupRoles: bootstrap.GroupRoles,
			RequiredProjectionFields: bootstrap.RequiredProjectionFields,
		}
	}
	if contract.FormatVersion != "cloud-agents-platform-authority-contract/v1" || contract.ContractKind != "database_role_authority" {
		return nil, fail(CodeInvalidManifest, "authority-contract", "invalid authority contract identity", nil)
	}
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return &contract, nil
}

func DecodeGlobalTableAuthorityContract(data []byte) (*GlobalTableAuthorityContract, error) {
	var contract GlobalTableAuthorityContract
	if _, err := DecodeStrict(data, &contract); err != nil {
		return nil, err
	}
	if contract.FormatVersion != "cloud-agents-platform-global-table-authority/v1" || contract.ContractKind != "global_table_writer_authority" {
		return nil, fail(CodeInvalidManifest, "global-table-authority", "invalid global table authority contract identity", nil)
	}
	expected := []GlobalTableWriter{
		{Name: "schema_migrations", Writers: []string{MigrationOwnerRole}},
		{Name: "workload_database_principals", Writers: []string{"audited_bootstrap_function"}},
		{Name: "builtin_roles", Writers: []string{MigrationOwnerRole}},
		{Name: "builtin_role_permissions", Writers: []string{MigrationOwnerRole}},
	}
	if !reflect.DeepEqual(contract.Tables, expected) {
		return nil, fail(CodeInvalidManifest, "global-table-authority", "global table writer closure differs from manifest v1", nil)
	}
	return &contract, nil
}

// DescriptorClassifier requires both structural admission and the exact signed
// source descriptor. It therefore cannot silently broaden when the SQL changes.
type DescriptorClassifier struct {
	narrow      NarrowDDLClassifier
	byMigration map[string][]SQLStatementDescriptor
}

func NewDescriptorClassifier(bundle *RuntimeBundle) (*DescriptorClassifier, error) {
	classifier := &DescriptorClassifier{
		narrow:      NarrowDDLClassifier{SpecialDO: make(map[SpecialStatementIdentity]Digest)},
		byMigration: make(map[string][]SQLStatementDescriptor),
	}
	for _, entry := range bundle.Manifest.SchemaBundle.Migrations {
		raw := bundle.Files[entry.CatalogContract.Path]
		contract, err := DecodeCatalogContract(raw)
		if err != nil {
			return nil, err
		}
		if contract.SchemaHead != entry.ID && contract.SchemaHead != bundle.Manifest.SchemaBundle.SchemaHead {
			return nil, fail(CodeInvalidManifest, entry.ID, "catalog source descriptor head mismatch", nil)
		}
		found := false
		for _, source := range contract.SourceDescriptors {
			if source.MigrationID != entry.ID {
				continue
			}
			if source.SQLSHA256 != entry.SQLArtifact.SHA256 {
				return nil, fail(CodeInvalidManifest, entry.ID, "catalog source SQL digest mismatch", nil)
			}
			classifier.byMigration[entry.ID] = source.Statements
			for _, statement := range source.Statements {
				if statement.Classification.Command == "DO" && statement.Classification.SpecialCase != nil {
					expectedSpecial := fmt.Sprintf("%s:%d:%s", entry.ID, statement.Index, statement.SHA256)
					if *statement.Classification.SpecialCase != expectedSpecial {
						return nil, fail(CodeInvalidManifest, entry.ID, "DO special_case identity mismatch", nil)
					}
					classifier.narrow.SpecialDO[SpecialStatementIdentity{MigrationID: entry.ID, StatementIndex: int(statement.Index)}] = statement.SHA256
				} else if statement.Classification.SpecialCase != nil {
					return nil, fail(CodeInvalidManifest, entry.ID, "non-DO statement has a special_case", nil)
				}
			}
			found = true
			break
		}
		if !found {
			return nil, fail(CodeInvalidManifest, entry.ID, "catalog lacks this migration source descriptor", nil)
		}
		statements, err := SplitPostgreSQLStatements(bundle.Files[entry.SQLArtifact.Path])
		if err != nil {
			return nil, err
		}
		if len(statements) != len(classifier.byMigration[entry.ID]) {
			return nil, fail(CodeInvalidManifest, entry.ID, "signed statement descriptor count differs from SQL", nil)
		}
		for _, statement := range statements {
			if _, err := classifier.Classify(entry, statement); err != nil {
				return nil, err
			}
		}
	}
	return classifier, nil
}

func (classifier *DescriptorClassifier) Classify(entry MigrationEntry, statement SQLStatement) (StatementPlan, error) {
	descriptors := classifier.byMigration[entry.ID]
	if statement.Index < 0 || statement.Index >= len(descriptors) {
		return StatementPlan{}, fail(CodeInvalidSQL, entry.ID, "statement is absent from signed descriptor", nil)
	}
	expected := descriptors[statement.Index]
	if uint64(statement.Index) != expected.Index || uint64(statement.Start) != expected.Start || uint64(statement.End) != expected.End || statement.SHA256 != expected.SHA256 || expected.Classification.Profile != "postgresql-ddl-v1" {
		return StatementPlan{}, fail(CodeInvalidSQL, entry.ID, "statement offset, digest, or profile differs from signed descriptor", nil)
	}
	plan, err := classifier.narrow.Classify(entry, statement)
	if err != nil {
		return StatementPlan{}, err
	}
	normalizedKind := normalizeObjectKind(plan.ObjectKind)
	if plan.Command == "DO" && normalizedKind == "SCHEMA" {
		normalizedKind = "SCHEMA_BOOTSTRAP"
	}
	if normalizedKind != expected.Classification.ObjectKind || plan.Command != expected.Classification.Command || plan.TargetIdentity != expected.Classification.TargetIdentity || !equalOptionalString(plan.Grantee, expected.Classification.Grantee) {
		return StatementPlan{}, fail(CodeInvalidSQL, entry.ID, "structural classification differs from signed descriptor", nil)
	}
	return plan, nil
}

func normalizeObjectKind(kind string) string {
	if kind == "DEFAULT PRIVILEGES" {
		return "DEFAULT_PRIVILEGES"
	}
	return kind
}

// Queryer is the minimal catalog-query boundary implemented by pgx.Conn and pgx.Tx adapters.
type Queryer interface {
	Query(context.Context, string, ...any) (Rows, error)
	QueryRow(context.Context, string, ...any) Row
}

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type Row interface{ Scan(...any) error }

type AuthorityProjector interface {
	ProjectAuthority(context.Context, Queryer, int) (AuthorityProjection, error)
}

type CatalogProjector interface {
	ProjectCatalog(context.Context, Queryer, int, string) (CatalogProjection, error)
	ProjectInitial(context.Context, Queryer, int, CatalogPrecondition) (CatalogProjection, error)
}

type AuthorityValidator interface {
	ValidateAuthority(context.Context, Queryer, int, []byte) (AuthorityProjection, error)
}

type CatalogValidator interface {
	ValidateCatalog(context.Context, Queryer, int, []byte, string) (CatalogProjection, error)
	ValidatePredecessor(context.Context, Queryer, int, CatalogPrecondition, map[string][]byte) (CatalogProjection, error)
}

// FailClosedAuthorityValidator documents the typed seam without pretending the
// NOT_IMPLEMENTED contract contains an executable PG15-17 expected projection.
type FailClosedAuthorityValidator struct{ Projector AuthorityProjector }

func (validator FailClosedAuthorityValidator) ValidateAuthority(ctx context.Context, queryer Queryer, major int, raw []byte) (AuthorityProjection, error) {
	contract, err := DecodeAuthorityContract(raw)
	if err != nil {
		return AuthorityProjection{}, err
	}
	if contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || contract.RuntimeIntrospectionStatus != "IMPLEMENTED" || validator.Projector == nil {
		return AuthorityProjection{}, fail(CodeUnsupported, "authority-projection", "signed authority contract is not executable yet", nil)
	}
	return AuthorityProjection{}, fail(CodeUntrusted, "authority-projection", "verified authority profile and deployment binding are unavailable", nil)
}

type FailClosedCatalogValidator struct{ Projector CatalogProjector }

func (validator FailClosedCatalogValidator) ValidateCatalog(ctx context.Context, queryer Queryer, major int, raw []byte, head string) (CatalogProjection, error) {
	contract, err := DecodeCatalogContract(raw)
	if err != nil {
		return CatalogProjection{}, err
	}
	if contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || contract.RuntimeIntrospectionStatus != "IMPLEMENTED" || validator.Projector == nil || contract.SchemaHead != head {
		return CatalogProjection{}, fail(CodeUnsupported, "catalog-projection", "signed catalog contract is not executable for this head yet", nil)
	}
	return CatalogProjection{}, fail(CodeUntrusted, "catalog-projection", "verified catalog contract is unavailable", nil)
}

func (validator FailClosedCatalogValidator) ValidatePredecessor(ctx context.Context, queryer Queryer, major int, condition CatalogPrecondition, files map[string][]byte) (CatalogProjection, error) {
	if condition.Artifact != nil {
		raw, ok := files[condition.Artifact.Path]
		if !ok {
			return CatalogProjection{}, fail(CodeInvalidArtifact, condition.Artifact.Path, "predecessor catalog artifact is missing", nil)
		}
		contract, err := DecodeCatalogContract(raw)
		if err != nil {
			return CatalogProjection{}, err
		}
		return validator.ValidateCatalog(ctx, queryer, major, raw, contract.SchemaHead)
	}
	return CatalogProjection{}, fail(CodeUntrusted, "catalog-projection", "verified schema bundle scope is unavailable", nil)
}

type IntermediateValidator interface {
	ValidateIntermediate(context.Context, Queryer, int, MigrationEntry, SQLStatement, StatementPlan, CatalogProjection) error
}

type FailClosedIntermediateValidator struct{}

func (FailClosedIntermediateValidator) ValidateIntermediate(context.Context, Queryer, int, MigrationEntry, SQLStatement, StatementPlan, CatalogProjection) error {
	return fail(CodeUnsupported, "catalog-intermediate", "per-statement owner/default-ACL transition validation is not implemented", nil)
}

func equalAuthorityProjection(left, right AuthorityProjection) bool {
	return reflect.DeepEqual(left, right)
}
func equalCatalogProjection(left, right CatalogProjection) bool {
	return reflect.DeepEqual(left, right)
}
