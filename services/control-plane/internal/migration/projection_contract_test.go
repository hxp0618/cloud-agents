package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const projectionTestDigest Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestObjectIdentityProjectionIsClosedUnion(t *testing.T) {
	t.Parallel()
	valid := []string{
		`{"kind":"schema","name":"cloud_agents"}`,
		`{"kind":"relation","identity":{"schema":"cloud_agents","name":"jobs"}}`,
		`{"kind":"function","identity":{"schema":"cloud_agents","name":"f","arguments":[{"schema":"pg_catalog","name":"text"}]}}`,
		`{"kind":"trigger","relation":{"schema":"cloud_agents","name":"jobs"},"name":"jobs_trigger","owning_constraint":null}`,
		`{"kind":"internal","semantic_kind":"row_type","owning_object":{"kind":"relation","identity":{"schema":"cloud_agents","name":"jobs"}}}`,
	}
	for _, raw := range valid {
		var identity ObjectIdentityProjection
		if _, err := DecodeStrict([]byte(raw), &identity); err != nil {
			t.Errorf("valid identity %s: %v", raw, err)
			continue
		}
		if err := identity.Validate(); err != nil {
			t.Errorf("validated identity %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"kind":"schema","name":"cloud_agents","oid":1}`,
		`{"kind":"unknown","name":"cloud_agents"}`,
		`{"kind":"function","identity":{"schema":"cloud_agents","name":"f","arguments":[]},"arguments":[]}`,
		`{"kind":"internal","semantic_kind":"nested","owning_object":{"kind":"internal","semantic_kind":"row_type","owning_object":{"kind":"schema","name":"cloud_agents"}}}`,
		`{"kind":"trigger","relation":{"schema":"cloud_agents","name":"jobs"},"name":"jobs_trigger","owning_constraint":{"kind":"schema","name":"cloud_agents"}}`,
	}
	for _, raw := range invalid {
		var identity ObjectIdentityProjection
		_, err := DecodeStrict([]byte(raw), &identity)
		if err == nil {
			err = identity.Validate()
		}
		if err == nil {
			t.Errorf("accepted invalid identity %s", raw)
		}
	}
}

func TestCatalogStateAndTransitionStrictShapes(t *testing.T) {
	t.Parallel()
	migrationID := "000001"
	head := "000001"
	prefix := uint32(0)
	predecessor := ProjectionScope{ScopeKind: "predecessor", SchemaHead: nil, MigrationID: &migrationID, ThroughStatementIndex: nil, DeclaredObjects: []ObjectIdentityProjection{}}
	statementPrefix := ProjectionScope{ScopeKind: "statement_prefix", SchemaHead: nil, MigrationID: &migrationID, ThroughStatementIndex: &prefix, DeclaredObjects: []ObjectIdentityProjection{}}
	final := ProjectionScope{ScopeKind: "final", SchemaHead: &head, MigrationID: nil, ThroughStatementIndex: nil, DeclaredObjects: []ObjectIdentityProjection{}}

	absent := CatalogStateProjection{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: predecessor, Schema: "cloud_agents"}}
	raw, err := json.Marshal(absent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CatalogStateProjection
	if _, err := DecodeStrict(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}

	present := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: final, Body: minimalCatalogBody()}}
	raw, err = json.Marshal(present)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStrict(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := present.ComputeDigest(); err != nil {
		t.Fatal(err)
	}

	transition := ExpectedStatementTransition{
		Profile:           StatementTransitionProfile,
		CatalogBefore:     CatalogStateDigestRef{Scope: predecessor, StateKind: "schema_absent", Digest: projectionTestDigest},
		CatalogAfter:      CatalogStateDigestRef{Scope: statementPrefix, StateKind: "schema_present", Digest: projectionTestDigest},
		AuthorityRelation: "unchanged_relative_to_verified_binding",
		ControlPlaneDelta: []ObjectTransitionProjection{},
	}
	if err := transition.Validate(); err != nil {
		t.Fatal(err)
	}
	transitionRaw, err := json.Marshal(transition)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(transitionRaw), `"state_kind":"schema_absent"`, `"state_kind":"schema_absent","body":{}`, 1)
	var strictTransition ExpectedStatementTransition
	if _, err := DecodeStrict([]byte(mutated), &strictTransition); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("transition digest ref accepted embedded state: %v", err)
	}
	badScope := transition
	badScope.CatalogAfter.Scope.ThroughStatementIndex = nil
	if err := badScope.Validate(); err == nil {
		t.Fatal("statement prefix without through_statement_index was accepted")
	}
	maxUint32Scope := []byte(`{"scope_kind":"statement_prefix","schema_head":null,"migration_id":"000001","through_statement_index":4294967295,"declared_objects":[]}`)
	var decodedScope ProjectionScope
	if _, err := DecodeStrict(maxUint32Scope, &decodedScope); err != nil {
		t.Fatalf("uint32 maximum scope was rejected: %v", err)
	}
	overflowScope := bytes.Replace(maxUint32Scope, []byte("4294967295"), []byte("4294967296"), 1)
	if _, err := DecodeStrict(overflowScope, &decodedScope); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("uint32 overflow scope was accepted: %v", err)
	}
}

func TestProjectionCrossSurfaceFaultsFailClosed(t *testing.T) {
	t.Parallel()
	schemaIdentity := ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: "cloud_agents"}}
	migrationID := "000001"
	mismatched := CatalogStateProjection{Present: &SchemaPresentProjection{
		State: "schema_present",
		Scope: ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}},
		Body:  minimalCatalogBody(),
	}}
	mismatched.Present.Body.DeclaredObjects = []ObjectIdentityProjection{schemaIdentity}
	mismatched.Present.Body.ObjectCount = 1
	if err := mismatched.Validate(); err == nil {
		t.Fatal("catalog state accepted different scope/body declared closures")
	}

	withRelation := minimalCatalogBody()
	withRelation.Relations = []RelationProjection{{}}
	if err := withRelation.Validate(); err == nil {
		t.Fatal("A2.1a namespace body accepted a relation projection")
	}
	duplicateDependencies := minimalCatalogBody()
	dependency := DependencyProjection{Depender: schemaIdentity, DependedOn: schemaIdentity, DependencyKind: "normal"}
	duplicateDependencies.Dependencies = []DependencyProjection{dependency, dependency}
	if err := duplicateDependencies.Validate(); err == nil {
		t.Fatal("catalog body accepted duplicate dependencies")
	}
	duplicateDenied := minimalCatalogBody()
	denied := DeniedObjectProjection{Object: schemaIdentity, ReasonCode: "undeclared_object"}
	duplicateDenied.DeniedObjects = []DeniedObjectProjection{denied, denied}
	if err := duplicateDenied.Validate(); err == nil {
		t.Fatal("catalog body accepted duplicate denied objects")
	}

	badOrigin := minimalCatalogBody()
	badOrigin.Schema.ExplicitACL = ACLSetProjection{CatalogValue: "explicit", Entries: []ACLProjection{{Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole, Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "default_acl_catalog"}}}
	if err := badOrigin.Validate(); err == nil {
		t.Fatal("schema ACL accepted a default-ACL-only origin")
	}

	configured := minimalAuthorityProjection(AuthorityPhaseConnectedSession)
	configured.Roles = []RoleProjection{{Name: MigrationOwnerRole, ConnectionLimitInt32Decimal: "-1", Config: []string{"search_path=pg_catalog"}}}
	if err := configured.Validate(); err == nil {
		t.Fatal("initial A2.1a authority accepted non-empty role config")
	}
}

func TestCatalogPreconditionRejectsLegacyUnknownAndMixedStates(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "golden")
	absent := string(mustRead(t, filepath.Join(root, "catalog-state-schema-absent-v1.json")))
	present := string(mustRead(t, filepath.Join(root, "catalog-state-schema-present-v1.json")))
	valid := []byte(`{"accepted_states":[` + absent + `,` + present + `]}`)
	var condition CatalogPrecondition
	if _, err := DecodeStrict(valid, &condition); err != nil {
		t.Fatal(err)
	}
	if condition.Artifact != nil || len(condition.AcceptedStates) != 2 || !validInitialCatalogStates("000001", condition.AcceptedStates) {
		t.Fatal("scoped absent/present predecessor pair lost its typed union")
	}

	legacy := strings.Replace(string(valid), `"schema_present"`, `"empty_schema"`, 1)
	if _, err := DecodeStrict([]byte(legacy), &condition); err == nil {
		t.Fatal("legacy empty_schema predecessor was accepted")
	}
	unknown := strings.Replace(string(valid), `"schema": "cloud_agents"`, `"schema": "cloud_agents","unknown":true`, 1)
	if _, err := DecodeStrict([]byte(unknown), &condition); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("unknown predecessor field was accepted: %v", err)
	}
	mixed := strings.Replace(string(valid), `"schema": "cloud_agents"`, `"schema": "cloud_agents","body":{}`, 1)
	if _, err := DecodeStrict([]byte(mixed), &condition); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("mixed absent/present predecessor branch was accepted: %v", err)
	}
}

func TestACLSetAndReachabilityPreserveDistinctFacts(t *testing.T) {
	t.Parallel()
	if err := (ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{{Grantor: "owner", Grantee: "role", Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "catalog_explicit"}}}).Validate(); err == nil {
		t.Fatal("null ACL with entries was accepted")
	}
	if err := (ACLSetProjection{CatalogValue: "explicit", Entries: []ACLProjection{{Grantee: "role", Privileges: []string{"USAGE"}, Grantable: []string{}, Origin: "catalog_explicit"}}}).Validate(); err == nil {
		t.Fatal("ACL without grantor provenance was accepted")
	}
	depth := uint32(1)
	witness := []string{"cloud_agents_runtime", "login"}
	reachability := ReachabilityProjection{
		Role: "cloud_agents_runtime", Member: "login",
		Privileges: []ReachabilityPrivilegeProjection{
			{PrivilegeKind: "member", Reachable: true, MinDepth: &depth, CanonicalWitness: &witness, EdgeCount: 1},
			{PrivilegeKind: "usage", Reachable: false, MinDepth: nil, CanonicalWitness: nil, EdgeCount: 0},
			{PrivilegeKind: "set", Reachable: true, MinDepth: &depth, CanonicalWitness: &witness, EdgeCount: 1},
		},
	}
	if err := reachability.Validate(); err != nil {
		t.Fatal(err)
	}
	reachability.Privileges[1].PrivilegeKind = "member"
	if err := reachability.Validate(); err == nil {
		t.Fatal("collapsed MEMBER/USAGE/SET reachability was accepted")
	}
}

func TestProjectionNumericProfiles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		bits  int
	}{
		{"0", 16}, {"-32768", 16}, {"32767", 16},
		{"-2147483648", 32}, {"2147483647", 32},
		{"-9223372036854775808", 64}, {"9223372036854775807", 64},
	} {
		if _, err := ValidateSignedIntegerDecimal(test.value, test.bits); err != nil {
			t.Errorf("valid int%d %q: %v", test.bits, test.value, err)
		}
	}
	for _, value := range []string{"-0", "+1", "01", "-01", "32768", "1.0", "9223372036854775808"} {
		bits := 64
		if value == "32768" {
			bits = 16
		}
		if _, err := ValidateSignedIntegerDecimal(value, bits); err == nil {
			t.Errorf("accepted invalid int%d %q", bits, value)
		}
	}

	for _, value := range []string{"0", "1", "-1", "1.23", "0.01", "-0.125", "-999999999999999999999.0001"} {
		if err := ValidateExactNumeric(value); err != nil {
			t.Errorf("valid numeric %q: %v", value, err)
		}
	}
	for _, value := range []string{"-0", "0.0", "1.20", "1.", ".1", "+1", "1e2", strings.Repeat("1", 129)} {
		if err := ValidateExactNumeric(value); err == nil {
			t.Errorf("accepted invalid numeric %q", value)
		}
	}
	for input, expected := range map[string]string{"0.0": "0", "1.2300": "1.23", "-10.500": "-10.5"} {
		canonical, err := CanonicalExactNumeric(input)
		if err != nil || canonical != expected {
			t.Errorf("canonical numeric %q: got %q, err=%v", input, canonical, err)
		}
	}
	if _, err := CanonicalExactNumeric("-0.0"); err == nil {
		t.Fatal("canonicalizer accepted negative zero")
	}

	float32Values := []string{"0", "-0.5", "1.0000001", normalizeRyuExponent(strconv.FormatFloat(math.SmallestNonzeroFloat32, 'g', -1, 32)), normalizeRyuExponent(strconv.FormatFloat(math.MaxFloat32, 'g', -1, 32))}
	for _, value := range float32Values {
		if err := ValidateRyuFloat32(value); err != nil {
			t.Errorf("valid float32 %q: %v", value, err)
		}
	}
	float64Values := []string{"0", "-0.5", "-0.125", "1e20", normalizeRyuExponent(strconv.FormatFloat(math.SmallestNonzeroFloat64, 'g', -1, 64)), normalizeRyuExponent(strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64))}
	for _, value := range float64Values {
		if err := ValidateRyuFloat64(value); err != nil {
			t.Errorf("valid float64 %q: %v", value, err)
		}
	}
	for _, value := range []string{"-0", "1.0", "1e+20", "1e020", "NaN", "Infinity", "5e-325", "0.000"} {
		if err := ValidateRyuFloat64(value); err == nil {
			t.Errorf("accepted invalid Ryu float %q", value)
		}
	}
}

func TestAuthorityBindingRequiresThreeCompletePhases(t *testing.T) {
	t.Parallel()
	binding := AuthorityBinding{
		FormatVersion: AuthorityBindingFormat, AuthorityProfileDigest: projectionTestDigest,
		DeploymentID: "deployment_1", IssuedAt: "2026-08-11T00:00:00Z", ExpiresAt: "2026-08-12T00:00:00Z", SecurityEpoch: 1,
		ExpectedProjections: AuthorityExpectedProjections{
			ConnectedSession:     minimalAuthorityProjection(AuthorityPhaseConnectedSession),
			MigrationRole:        minimalAuthorityProjection(AuthorityPhaseMigrationRole),
			MigrationTransaction: minimalAuthorityProjection(AuthorityPhaseMigrationTransaction),
		},
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAuthorityBinding(raw); err != nil {
		t.Fatal(err)
	}
	binding.ExpectedProjections.MigrationRole.Phase = AuthorityPhaseConnectedSession
	raw, err = json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAuthorityBinding(raw); err == nil {
		t.Fatal("binding accepted a duplicated phase")
	}
	mutated := strings.Replace(string(raw), `"security_epoch":1`, `"security_epoch":1,"trust_me":true`, 1)
	if _, err := DecodeAuthorityBinding([]byte(mutated)); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("binding accepted unknown field: %v", err)
	}
}

func TestIntermediateAndAttemptTerminalLegalCombinations(t *testing.T) {
	t.Parallel()
	intermediate := StatementIntermediateState{
		SchemaBundleDigest: projectionTestDigest, CatalogContractDigest: projectionTestDigest,
		AuthorityProfileDigest: projectionTestDigest, AuthorityBindingDigest: projectionTestDigest,
		MigrationID: "000001", AttemptIndex: 1, StatementIndex: 0, StatementSHA256: projectionTestDigest,
		PreviousAttemptTerminalDigest: nil, PreviousIntermediateStateDigest: nil,
		ControlPlaneStates: ControlPlaneStates{
			TxStatus: "T", SessionUser: "migration_login", CurrentUser: MigrationOwnerRole, MigrationRole: MigrationOwnerRole,
			AdvisoryLock:                    AdvisoryLockProjection{Domain: AdvisoryLockDomain, KeyInt64Decimal: "-1047838957622507638", Held: true},
			VerifiedAuthorityDecisionDigest: projectionTestDigest, SchemaOwner: MigrationOwnerRole,
			SchemaExplicitACLDigest: projectionTestDigest, SchemaEffectiveACLDigest: projectionTestDigest,
			DefaultACLDigest: projectionTestDigest, ExpectedTransitionDigest: projectionTestDigest,
		},
		AuthorityBeforeDigest: projectionTestDigest, AuthorityAfterDigest: projectionTestDigest,
		CatalogBeforeDigest: projectionTestDigest, CatalogAfterDigest: projectionTestDigest,
	}
	digest, err := intermediate.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	intermediate.IntermediateStateDigest = digest
	if err := intermediate.Validate(); err != nil {
		t.Fatal(err)
	}
	badIntermediate := intermediate
	linkDigest := projectionTestDigest
	badIntermediate.PreviousIntermediateStateDigest = &linkDigest
	if err := badIntermediate.Validate(); err == nil {
		t.Fatal("first statement linked a previous intermediate state")
	}

	terminal := AttemptTerminalState{
		SchemaBundleDigest: projectionTestDigest, CatalogContractDigest: projectionTestDigest,
		AuthorityProfileDigest: projectionTestDigest, AuthorityBindingDigest: projectionTestDigest,
		MigrationID: "000001", AttemptIndex: 1, PreviousAttemptTerminalDigest: nil,
		LastIntermediateStateDigest: &intermediate.IntermediateStateDigest,
		Outcome:                     "committed", StableErrorCode: nil, ReconcileResult: "not_run",
	}
	terminalDigest, err := terminal.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	terminal.TerminalDigest = terminalDigest
	if err := terminal.Validate(3); err != nil {
		t.Fatal(err)
	}

	errorCode := "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED"
	retryable := terminal
	retryable.Outcome = "aborted_retryable"
	retryable.StableErrorCode = &errorCode
	retryable.LastIntermediateStateDigest = nil
	retryable.AttemptIndex = 3
	retryable.PreviousAttemptTerminalDigest = &linkDigest
	retryable.TerminalDigest, err = retryable.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := retryable.Validate(3); err == nil {
		t.Fatal("retryable outcome at max_attempts was accepted")
	}

	divergent := terminal
	divergent.Outcome = "ambiguous_divergent"
	divergent.StableErrorCode = &errorCode
	divergent.ReconcileResult = "exact_pending"
	divergent.TerminalDigest, err = divergent.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := divergent.Validate(3); err == nil {
		t.Fatal("divergent outcome with exact_pending reconcile result was accepted")
	}
	ambiguousCommitted := terminal
	ambiguousCommitted.Outcome = "ambiguous_reconciled_committed"
	ambiguousCommitted.StableErrorCode = &errorCode
	ambiguousCommitted.ReconcileResult = "exact_committed"
	ambiguousCommitted.LastIntermediateStateDigest = nil
	ambiguousCommitted.TerminalDigest, err = ambiguousCommitted.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := ambiguousCommitted.Validate(3); err == nil {
		t.Fatal("ambiguous reconciled commit without last intermediate digest was accepted")
	}
}

func TestFailClosedValidatorsCannotBypassVerifiedTrust(t *testing.T) {
	t.Parallel()
	authorityProjector := &recordingAuthorityProjector{}
	authorityRaw := mustJSON(t, AuthorityContract{
		FormatVersion: "cloud-agents-platform-authority-contract/v1", ContractKind: "database_role_authority",
		PublicationStatus: "PUBLISHED_IMMUTABLE", RuntimeIntrospectionStatus: "IMPLEMENTED",
		Database:                 AuthorityDatabaseContract{Encoding: "UTF8", LocaleProvider: "libc", Datcollate: "C", Datctype: "C"},
		GroupRoles:               []string{MigrationOwnerRole, RuntimeRole, BootstrapAdminRole},
		RequiredProjectionFields: authorityProjectionFieldClosure(), RequiredBindingFields: authorityBindingFieldClosure(),
	})
	if _, err := (FailClosedAuthorityValidator{Projector: authorityProjector}).ValidateAuthority(context.Background(), nil, 17, authorityRaw); !IsCode(err, CodeUntrusted) {
		t.Fatalf("authority validator did not reject missing verified binding: %v", err)
	}
	if authorityProjector.called {
		t.Fatal("unverified authority contract reached projector")
	}

	catalogProjector := &recordingCatalogProjector{}
	catalogRaw := mustJSON(t, minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED"))
	if _, err := (FailClosedCatalogValidator{Projector: catalogProjector}).ValidateCatalog(context.Background(), nil, 17, catalogRaw, "000001"); !IsCode(err, CodeUntrusted) {
		t.Fatalf("catalog validator did not reject missing verified catalog subject: %v", err)
	}
	if catalogProjector.called {
		t.Fatal("unverified catalog contract reached projector")
	}
}

func TestExecutableContractsCannotUseBootstrapSparseShape(t *testing.T) {
	t.Parallel()
	fullCatalog := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
	raw := mustJSON(t, fullCatalog)
	object := map[string]any{}
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	delete(object, "expected_projection")
	sparse := mustJSON(t, object)
	if _, err := DecodeCatalogContract(sparse); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("executable catalog accepted sparse projection shape: %v", err)
	}

	bootstrap := object
	bootstrap["publication_status"] = "UNPUBLISHED_BOOTSTRAP_MUTABLE"
	bootstrap["runtime_introspection_status"] = "NOT_IMPLEMENTED"
	bootstrap["executable_expected_projection_status"] = "NOT_IMPLEMENTED_A2_1B_REQUIRED"
	bootstrap["projection_model"] = map[string]any{
		"projection_slice":          "A2.1a_namespace_only",
		"catalog_projection_fields": []string{"schema_head", "body"},
		"body_fields":               []string{"schema", "default_acl", "relations", "functions", "dependencies", "object_count", "declared_objects", "denied_objects"},
		"schema_fields":             []string{"name", "owner", "explicit_acl", "effective_acl", "comment", "security_labels"},
		"default_acl_fields":        []string{"owner", "schema", "object_kind", "acl"},
		"acl_set_fields":            []string{"catalog_value", "entries"},
		"acl_entry_fields":          []string{"grantor", "grantee", "privileges", "grantable", "origin"},
		"deferred_to_a2_1b":         []string{"relation_projection", "function_projection", "dependency_projection", "expression_projection"},
	}
	for _, source := range bootstrap["source_descriptors"].([]any) {
		for _, statement := range source.(map[string]any)["statements"].([]any) {
			delete(statement.(map[string]any), "expected_transition")
		}
	}
	if _, err := DecodeCatalogContract(mustJSON(t, bootstrap)); err != nil {
		t.Fatalf("bootstrap compatibility shape was rejected: %v", err)
	}

	fullAuthority := AuthorityContract{
		FormatVersion: "cloud-agents-platform-authority-contract/v1", ContractKind: "database_role_authority",
		PublicationStatus: "PUBLISHED_IMMUTABLE", RuntimeIntrospectionStatus: "IMPLEMENTED",
		Database:                 AuthorityDatabaseContract{Encoding: "UTF8", LocaleProvider: "libc", Datcollate: "C", Datctype: "C"},
		GroupRoles:               []string{MigrationOwnerRole, RuntimeRole, BootstrapAdminRole},
		RequiredProjectionFields: authorityProjectionFieldClosure(), RequiredBindingFields: authorityBindingFieldClosure(),
	}
	authorityObject := map[string]any{}
	if err := json.Unmarshal(mustJSON(t, fullAuthority), &authorityObject); err != nil {
		t.Fatal(err)
	}
	delete(authorityObject, "required_binding_fields")
	if _, err := DecodeAuthorityProfile(mustJSON(t, authorityObject)); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("executable authority profile accepted missing binding closure: %v", err)
	}
}

func TestCheckedInProjectionFixtureManifestSameBits(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection")
	manifestRaw := mustRead(t, filepath.Join(root, "manifest.json"))
	if DigestBytes(manifestRaw) != "sha256:63134576ea192601ad597deff09cbd82befa181bab0306d9a4e62831d3f68daf" {
		t.Fatal("projection fixture manifest differs from the reviewed TS same-bits bytes")
	}
	var manifest projectionFixtureManifest
	if _, err := DecodeStrict(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != "cloud-agents-platform-projection-fixtures/v1" || manifest.RuntimeAuthority || manifest.PublicationStatus != "UNPUBLISHED_BOOTSTRAP_MUTABLE" || manifest.RuntimeIntrospectionStatus != "NOT_IMPLEMENTED" {
		t.Fatal("projection fixture manifest attempts to claim runtime authority")
	}
	for _, head := range []string{"000001", "000002"} {
		catalog, err := DecodeCatalogContract(mustRead(t, filepath.Join(migrationRoot(t), "catalog", "schema-"+head+".json")))
		if err != nil {
			t.Fatalf("bootstrap catalog %s: %s", head, projectionErrorChain(err))
		}
		if !catalog.bootstrapShape || catalog.bootstrapExecutableStatus != "NOT_IMPLEMENTED_A2_1B_REQUIRED" {
			t.Fatalf("bootstrap catalog %s lost the explicit A2.1b executable boundary", head)
		}
	}
	files := make(map[string][]byte, len(manifest.Files))
	for _, record := range manifest.Files {
		raw := mustRead(t, filepath.Join(root, filepath.FromSlash(record.Path)))
		if uint64(len(raw)) != record.SizeBytes || DigestBytes(raw) != record.SHA256 {
			t.Fatalf("projection fixture %s differs from manifest same-bits record", record.Path)
		}
		files[record.Path] = raw
	}

	authorityProfileRaw := mustRead(t, filepath.Join(migrationRoot(t), "catalog", "authority-v1.json"))
	if _, err := DecodeAuthorityProfile(authorityProfileRaw); err != nil {
		t.Fatal(err)
	}
	binding, err := DecodeAuthorityBinding(files["golden/authority-binding-v1.json"])
	if err != nil {
		t.Fatal(err)
	}
	if binding.AuthorityProfileDigest != canonicalRawDigest(t, authorityProfileRaw) {
		t.Fatal("authority binding profile digest differs from checked-in profile canonical bytes")
	}
	assertCanonicalRoundTrip(t, files["golden/authority-binding-v1.json"], binding)

	var body CatalogProjectionBody
	if _, err := DecodeStrict(files["golden/catalog-projection-body-v1.json"], &body); err != nil {
		t.Fatal(err)
	}
	if err := body.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalRoundTrip(t, files["golden/catalog-projection-body-v1.json"], body)

	var absent CatalogStateProjection
	if _, err := DecodeStrict(files["golden/catalog-state-schema-absent-v1.json"], &absent); err != nil {
		t.Fatal(err)
	}
	absentDigest, err := absent.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalRoundTrip(t, files["golden/catalog-state-schema-absent-v1.json"], absent)

	var present CatalogStateProjection
	if _, err := DecodeStrict(files["golden/catalog-state-schema-present-v1.json"], &present); err != nil {
		t.Fatal(err)
	}
	if _, err := present.ComputeDigest(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalRoundTrip(t, files["golden/catalog-state-schema-present-v1.json"], present)

	var transition ExpectedStatementTransition
	if _, err := DecodeStrict(files["golden/expected-statement-transition-v1.json"], &transition); err != nil {
		t.Fatal(err)
	}
	transitionDigest, err := transition.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if transition.CatalogBefore.Digest != absentDigest {
		t.Fatal("expected transition predecessor digest differs from Go catalog-state same-bits digest")
	}
	statementState := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: transition.CatalogAfter.Scope, Body: body}}
	statementDigest, err := statementState.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if statementDigest != transition.CatalogAfter.Digest {
		t.Fatal("expected transition after digest differs from Go catalog-state same-bits digest")
	}
	assertCanonicalRoundTrip(t, files["golden/expected-statement-transition-v1.json"], transition)

	var intermediate StatementIntermediateState
	if _, err := DecodeStrict(files["golden/intermediate-state-v1.json"], &intermediate); err != nil {
		t.Fatal(err)
	}
	if err := intermediate.Validate(); err != nil {
		t.Fatal(err)
	}
	if intermediate.ControlPlaneStates.ExpectedTransitionDigest != transitionDigest || intermediate.CatalogBeforeDigest != transition.CatalogBefore.Digest || intermediate.CatalogAfterDigest != transition.CatalogAfter.Digest {
		t.Fatal("intermediate state is not bound to the checked-in expected transition")
	}
	if intermediate.AuthorityProfileDigest != binding.AuthorityProfileDigest || intermediate.AuthorityBindingDigest != canonicalRawDigest(t, files["golden/authority-binding-v1.json"]) {
		t.Fatal("intermediate state authority profile/binding digest differs from canonical fixture bytes")
	}
	assertCanonicalRoundTrip(t, files["golden/intermediate-state-v1.json"], intermediate)

	var terminal AttemptTerminalState
	if _, err := DecodeStrict(files["golden/attempt-terminal-state-v1.json"], &terminal); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Validate(3); err != nil {
		t.Fatal(err)
	}
	if terminal.LastIntermediateStateDigest == nil || *terminal.LastIntermediateStateDigest != intermediate.IntermediateStateDigest {
		t.Fatal("attempt terminal state does not close the checked-in intermediate chain")
	}
	assertCanonicalRoundTrip(t, files["golden/attempt-terminal-state-v1.json"], terminal)

	validateCheckedInNumericFixture(t, files["golden/numeric-v1.json"])
	if _, err := DecodeAuthorityBinding(files["negative/authority-binding-duplicate.raw"]); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("duplicate-key authority fixture was accepted: %v", err)
	}
	var faults projectionFaultManifest
	if _, err := DecodeStrict(files["negative/faults-v1.json"], &faults); err != nil {
		t.Fatal(err)
	}
	if faults.FormatVersion != "cloud-agents-platform-projection-faults/v1" || len(faults.Cases) != 32 {
		t.Fatal("projection fault manifest is empty or has the wrong profile")
	}
}

func FuzzProjectionContractDecodersNeverPanic(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"kind":"schema","name":"cloud_agents"}`),
		[]byte(`{"state":"schema_absent","scope":{"scope_kind":"predecessor","schema_head":null,"migration_id":"000001","through_statement_index":null,"declared_objects":[]},"schema":"cloud_agents"}`),
		[]byte(`{"kind":"internal","semantic_kind":"nested","owning_object":{"kind":"internal"}}`),
		{0xff},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		var identity ObjectIdentityProjection
		_, _ = DecodeStrict(input, &identity)
		var state CatalogStateProjection
		_, _ = DecodeStrict(input, &state)
		_, _ = DecodeAuthorityBinding(input)
	})
}

func minimalAuthorityProjection(phase AuthorityPhase) AuthorityProjection {
	return AuthorityProjection{
		Phase: phase, SessionUser: "migration_login", CurrentUser: MigrationOwnerRole,
		DatabaseName: "cloud_agents", DatabaseOwner: "database_owner", DatabaseEncoding: "UTF8",
		LocaleProvider: "libc", Datcollate: "C", Datctype: "C",
		DatabaseACL: ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
		Roles:       []RoleProjection{}, DirectMemberships: []DirectMembershipProjection{},
		MembershipReachability: []ReachabilityProjection{}, DatabaseRoleSettings: []DatabaseRoleSettingProjection{},
		EffectiveCreate: map[string]bool{MigrationOwnerRole: true}, EffectiveTemporary: map[string]bool{MigrationOwnerRole: false},
	}
}

func authorityProjectionFieldClosure() []string {
	return []string{"phase", "session_user", "current_user", "database_name", "database_owner", "database_encoding", "locale_provider", "datcollate", "datctype", "icu_locale", "icu_rules", "collation_version", "database_acl", "roles", "direct_memberships", "membership_reachability", "database_role_settings", "effective_create", "effective_temporary"}
}

func authorityBindingFieldClosure() []string {
	return []string{"authority_profile_digest", "deployment_id", "issued_at", "expires_at", "security_epoch", "expected_projections"}
}

func minimalCatalogBody() CatalogProjectionBody {
	return CatalogProjectionBody{
		Schema: SchemaProjection{
			Name: "cloud_agents", Owner: MigrationOwnerRole,
			ExplicitACL:    ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
			EffectiveACL:   []ACLProjection{{Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole, Privileges: []string{"CREATE", "USAGE"}, Grantable: []string{"CREATE", "USAGE"}, Origin: "owner_implicit"}},
			SecurityLabels: []SecurityLabel{},
		},
		DefaultACL: []DefaultACLProjection{}, Relations: []RelationProjection{}, Functions: []FunctionProjection{},
		Dependencies: []DependencyProjection{}, ObjectCount: 0, DeclaredObjects: []ObjectIdentityProjection{}, DeniedObjects: []DeniedObjectProjection{},
	}
}

func minimalCatalogContract(t *testing.T, publicationStatus, introspectionStatus string) CatalogContract {
	t.Helper()
	body := minimalCatalogBody()
	head := "000001"
	migrationID := "000001"
	finalScope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: []ObjectIdentityProjection{}}
	finalDigest, err := (CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: finalScope, Body: body}}).ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	contract := CatalogContract{
		FormatVersion: "cloud-agents-platform-catalog/v1", ContractKind: "cumulative_schema_catalog", SchemaHead: "000001",
		PublicationStatus: publicationStatus, RuntimeIntrospectionStatus: introspectionStatus,
		SourceDescriptors: []SQLSourceDescriptor{{
			MigrationID: migrationID, SQLSHA256: projectionTestDigest,
			Statements: []SQLStatementDescriptor{{
				Index: 0, Start: 0, End: 1, SHA256: projectionTestDigest,
				Classification: SQLClassificationDescriptor{Profile: "postgresql-ddl-v1", Command: "CREATE", ObjectKind: "SCHEMA", TargetIdentity: "schema:unquoted:cloud_agents"},
				ExpectedTransition: ExpectedStatementTransition{
					Profile:           StatementTransitionProfile,
					CatalogBefore:     CatalogStateDigestRef{Scope: ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}, StateKind: "schema_absent", Digest: projectionTestDigest},
					CatalogAfter:      CatalogStateDigestRef{Scope: finalScope, StateKind: "schema_present", Digest: finalDigest},
					AuthorityRelation: "unchanged_relative_to_verified_binding", ControlPlaneDelta: []ObjectTransitionProjection{},
				},
			}},
		}},
		ProjectionModel: CatalogProjectionModel{
			SchemaFields: []string{}, DefaultACLFields: []string{}, RelationFields: []string{}, ColumnFields: []string{},
			ConstraintFields: []string{}, IndexFields: []string{}, PolicyFields: []string{}, TriggerFields: []string{},
			FunctionFields: []string{}, ExpressionProfile: "cloud-agents-sql-expression/v1", DeniedObjectKinds: []string{},
		},
		DeclaredObjectIdentities: []ObjectIdentityProjection{},
		ExpectedProjection:       CatalogProjection{SchemaHead: "000001", Body: body},
	}
	return contract
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type recordingAuthorityProjector struct{ called bool }

func (projector *recordingAuthorityProjector) ProjectAuthority(context.Context, Queryer, int) (AuthorityProjection, error) {
	projector.called = true
	return AuthorityProjection{}, nil
}

type recordingCatalogProjector struct{ called bool }

func (projector *recordingCatalogProjector) ProjectCatalog(context.Context, Queryer, int, string) (CatalogProjection, error) {
	projector.called = true
	return CatalogProjection{}, nil
}

type projectionFixtureManifest struct {
	FormatVersion              string                    `json:"format_version"`
	RuntimeAuthority           bool                      `json:"runtime_authority"`
	PublicationStatus          string                    `json:"publication_status"`
	RuntimeIntrospectionStatus string                    `json:"runtime_introspection_status"`
	Files                      []projectionFixtureRecord `json:"files"`
}

type projectionFixtureRecord struct {
	Path      string `json:"path"`
	SizeBytes uint64 `json:"size_bytes"`
	SHA256    Digest `json:"sha256"`
}

type projectionFaultManifest struct {
	FormatVersion string                `json:"format_version"`
	Cases         []projectionFaultCase `json:"cases"`
}

type projectionFaultCase struct {
	Name          string `json:"name"`
	Target        string `json:"target"`
	Mutation      string `json:"mutation"`
	ExpectedError string `json:"expected_error"`
}

type projectionNumericFixture struct {
	FormatVersion string                        `json:"format_version"`
	SignedInteger []projectionSignedIntegerCase `json:"signed_integer"`
	ExactNumeric  []projectionExactNumericCase  `json:"exact_numeric"`
	Float         []projectionFloatCase         `json:"float"`
}

type projectionSignedIntegerCase struct {
	Bits          uint64  `json:"bits"`
	Input         string  `json:"input"`
	Expected      *string `json:"expected"`
	ExpectedError *string `json:"expected_error"`
}

type projectionExactNumericCase struct {
	Input         string  `json:"input"`
	Expected      *string `json:"expected"`
	ExpectedError *string `json:"expected_error"`
}

type projectionFloatCase struct {
	Kind          string  `json:"kind"`
	Input         string  `json:"input"`
	Expected      *string `json:"expected"`
	ExpectedError *string `json:"expected_error"`
}

func validateCheckedInNumericFixture(t *testing.T, raw []byte) {
	t.Helper()
	var fixture projectionNumericFixture
	if _, err := DecodeStrict(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.FormatVersion != "cloud-agents-platform-projection-numeric-fixtures/v1" {
		t.Fatal("unknown numeric fixture format")
	}
	for _, test := range fixture.SignedInteger {
		parsed, err := ValidateSignedIntegerDecimal(test.Input, int(test.Bits))
		assertFixtureResult(t, strconv.FormatInt(parsed, 10), err, test.Expected, test.ExpectedError)
	}
	for _, test := range fixture.ExactNumeric {
		actual, err := CanonicalExactNumeric(test.Input)
		assertFixtureResult(t, actual, err, test.Expected, test.ExpectedError)
	}
	for _, test := range fixture.Float {
		var err error
		switch test.Kind {
		case "float4":
			err = ValidateRyuFloat32(test.Input)
		case "float8":
			err = ValidateRyuFloat64(test.Input)
		default:
			err = invalidProjection("numeric-fixture", "unknown float kind")
		}
		assertFixtureResult(t, test.Input, err, test.Expected, test.ExpectedError)
	}
}

func assertFixtureResult(t *testing.T, actual string, err error, expected, expectedError *string) {
	t.Helper()
	if expected != nil {
		if err != nil || actual != *expected {
			t.Fatalf("numeric fixture mismatch: got %q err=%v, expected %q", actual, err, *expected)
		}
		return
	}
	if expectedError == nil || err == nil {
		t.Fatalf("numeric fixture expected rejection %v, got %q err=%v", expectedError, actual, err)
	}
}

func canonicalRawDigest(t *testing.T, raw []byte) Digest {
	t.Helper()
	value, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return DigestBytes(canonical)
}

func assertCanonicalRoundTrip(t *testing.T, raw []byte, typed any) {
	t.Helper()
	typedRaw, err := json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := ParseStrictJSON(typedRaw)
	if err != nil {
		t.Fatal(err)
	}
	originalCanonical, err := CanonicalJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	roundTripCanonical, err := CanonicalJSON(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(originalCanonical, roundTripCanonical) {
		t.Fatal("typed fixture round-trip differs from checked-in canonical JSON bytes")
	}
}

func projectionErrorChain(err error) string {
	parts := []string{}
	for err != nil {
		parts = append(parts, err.Error())
		type unwrapper interface{ Unwrap() error }
		wrapped, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = wrapped.Unwrap()
	}
	return strings.Join(parts, " <- ")
}

func (projector *recordingCatalogProjector) ProjectInitial(context.Context, Queryer, int, CatalogPrecondition) (CatalogProjection, error) {
	projector.called = true
	return CatalogProjection{}, nil
}
