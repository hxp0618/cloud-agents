package migration

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCheckedInProjectionFaultCases(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection")
	var faults projectionFaultManifest
	if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "negative", "faults-v1.json")), &faults); err != nil {
		t.Fatal(err)
	}
	if len(faults.Cases) != 32 {
		t.Fatalf("expected 32 checked-in faults, got %d", len(faults.Cases))
	}
	seen := make(map[string]struct{}, len(faults.Cases))
	for _, fault := range faults.Cases {
		fault := fault
		t.Run(fault.Name, func(t *testing.T) {
			if _, duplicate := seen[fault.Name]; duplicate {
				t.Fatalf("duplicate checked-in fault name %q", fault.Name)
			}
			seen[fault.Name] = struct{}{}
			if fault.ExpectedError == "" {
				t.Fatal("checked-in fault lacks expected_error")
			}
			if err := executeCheckedInProjectionFault(t, root, fault); err == nil {
				t.Fatalf("mutation %q was accepted; expected %s", fault.Mutation, fault.ExpectedError)
			}
		})
	}
}

func executeCheckedInProjectionFault(t *testing.T, root string, fault projectionFaultCase) error {
	t.Helper()
	mutation := fault.Mutation
	switch mutation {
	case "unknown_field":
		if fault.Target == "expected_transition" {
			transition := faultObject(t, filepath.Join(root, "golden", "expected-statement-transition-v1.json"))
			transition["actual_projection"] = map[string]any{}
			return decodeFaultTransition(t, transition)
		}
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		binding["unsigned_overlay"] = true
		return decodeFaultAuthorityBinding(t, binding)
	case "missing_expires_at":
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		delete(binding, "expires_at")
		return decodeFaultAuthorityBinding(t, binding)
	case "duplicate_format_version":
		_, err := DecodeAuthorityBinding(mustRead(t, filepath.Join(root, "negative", "authority-binding-duplicate.raw")))
		return err
	case "phase_mismatch":
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		faultMap(faultMap(binding, "expected_projections"), "migration_transaction")["phase"] = "migration_role"
		return decodeFaultAuthorityBinding(t, binding)
	case "bad_profile_digest":
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		binding["authority_profile_digest"] = "sha256:ABC"
		return decodeFaultAuthorityBinding(t, binding)
	case "null_acl_with_entries":
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		projection := faultMap(faultMap(binding, "expected_projections"), "connected_session")
		faultMap(projection, "database_acl")["catalog_value"] = "null"
		return decodeFaultAuthorityBinding(t, binding)
	case "final_without_head":
		state := faultObject(t, filepath.Join(root, "golden", "catalog-state-schema-present-v1.json"))
		faultMap(state, "scope")["scope_kind"] = "final"
		return decodeFaultCatalogState(t, state)
	case "predecessor_with_head":
		state := faultObject(t, filepath.Join(root, "golden", "catalog-state-schema-absent-v1.json"))
		faultMap(state, "scope")["schema_head"] = "000000"
		return decodeFaultCatalogState(t, state)
	case "wrong_schema_name":
		state := faultObject(t, filepath.Join(root, "golden", "catalog-state-schema-absent-v1.json"))
		state["schema"] = "other"
		return decodeFaultCatalogState(t, state)
	case "scope_body_declared_mismatch":
		state := faultObject(t, filepath.Join(root, "golden", "catalog-state-schema-present-v1.json"))
		faultMap(state, "scope")["declared_objects"] = []any{map[string]any{"kind": "schema", "name": "cloud_agents"}}
		return decodeFaultCatalogState(t, state)
	case "relation_nonempty":
		body := faultObject(t, filepath.Join(root, "golden", "catalog-projection-body-v1.json"))
		body["relations"] = []any{map[string]any{}}
		return decodeFaultCatalogBody(t, body)
	case "duplicate_dependency":
		body := faultObject(t, filepath.Join(root, "golden", "catalog-projection-body-v1.json"))
		dependency := map[string]any{"depender": map[string]any{"kind": "schema", "name": "cloud_agents"}, "depended_on": map[string]any{"kind": "schema", "name": "pg_catalog"}, "dependency_kind": "normal"}
		body["dependencies"] = []any{dependency, cloneFaultObject(t, dependency)}
		return decodeFaultCatalogBody(t, body)
	case "duplicate_denied_object":
		body := faultObject(t, filepath.Join(root, "golden", "catalog-projection-body-v1.json"))
		denied := map[string]any{"object": map[string]any{"kind": "schema", "name": "unknown_schema"}, "owner": nil, "dependency_kind": nil, "depended_on": nil, "reason_code": "undeclared_object"}
		body["denied_objects"] = []any{denied, cloneFaultObject(t, denied)}
		return decodeFaultCatalogBody(t, body)
	case "trigger_owner_not_constraint":
		identity := map[string]any{"kind": "trigger", "relation": map[string]any{"schema": "cloud_agents", "name": "jobs"}, "name": "jobs_trigger", "owning_constraint": map[string]any{"kind": "schema", "name": "cloud_agents"}}
		return decodeFaultObjectIdentity(t, identity)
	case "bad_after_digest":
		transition := faultObject(t, filepath.Join(root, "golden", "expected-statement-transition-v1.json"))
		faultMap(transition, "catalog_after")["digest"] = "sha256:0"
		return decodeFaultTransition(t, transition)
	case "open_object_identity":
		transition := faultObject(t, filepath.Join(root, "golden", "expected-statement-transition-v1.json"))
		delta := faultSlice(transition, "control_plane_delta")
		faultMap(delta[0].(map[string]any), "object")["oid"] = float64(42)
		return decodeFaultTransition(t, transition)
	case "signed_overflow":
		_, err := ValidateSignedIntegerDecimal("9223372036854775808", 64)
		return err
	case "statement_index_overflow":
		intermediate := faultObject(t, filepath.Join(root, "golden", "intermediate-state-v1.json"))
		intermediate["statement_index"] = float64(4294967296)
		return decodeFaultIntermediate(t, intermediate)
	case "numeric_exponent":
		_, err := CanonicalExactNumeric("1e3")
		return err
	case "float_nan":
		return ValidateRyuFloat64("NaN")
	case "bad_digest":
		intermediate := faultObject(t, filepath.Join(root, "golden", "intermediate-state-v1.json"))
		intermediate["intermediate_state_digest"] = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		return decodeFaultIntermediate(t, intermediate)
	case "attempt_two_without_previous_terminal":
		if fault.Target == "attempt_terminal" {
			terminal := faultObject(t, filepath.Join(root, "golden", "attempt-terminal-state-v1.json"))
			terminal["attempt_index"] = float64(2)
			return decodeFaultTerminal(t, terminal)
		}
		intermediate := faultObject(t, filepath.Join(root, "golden", "intermediate-state-v1.json"))
		intermediate["attempt_index"] = float64(2)
		return decodeFaultIntermediate(t, intermediate)
	case "statement_one_without_previous_intermediate":
		intermediate := faultObject(t, filepath.Join(root, "golden", "intermediate-state-v1.json"))
		intermediate["statement_index"] = float64(1)
		return decodeFaultIntermediate(t, intermediate)
	case "wrong_advisory_domain":
		intermediate := faultObject(t, filepath.Join(root, "golden", "intermediate-state-v1.json"))
		faultMap(faultMap(intermediate, "control_plane_states"), "advisory_lock")["domain"] = "wrong"
		return decodeFaultIntermediate(t, intermediate)
	case "illegal_combination":
		terminal := faultObject(t, filepath.Join(root, "golden", "attempt-terminal-state-v1.json"))
		terminal["stable_error_code"] = "MIGRATION_LOCK_LOST"
		return decodeFaultTerminal(t, terminal)
	case "ambiguous_committed_without_last_digest":
		terminal := faultObject(t, filepath.Join(root, "golden", "attempt-terminal-state-v1.json"))
		terminal["outcome"] = "ambiguous_reconciled_committed"
		terminal["stable_error_code"] = "MIGRATION_AMBIGUOUS_COMMIT"
		terminal["reconcile_result"] = "exact_committed"
		terminal["last_intermediate_state_digest"] = nil
		return decodeFaultTerminal(t, terminal)
	case "icu_locale_nonnull":
		profile := faultObject(t, filepath.Join(migrationRoot(t), "catalog", "authority-v1.json"))
		faultMap(profile, "database")["icu_locale"] = "en-US"
		_, err := DecodeAuthorityProfile(faultJSON(t, profile))
		return err
	case "default_acl_catalog_explicit_origin":
		body := faultObject(t, filepath.Join(root, "golden", "catalog-projection-body-v1.json"))
		defaultACL := faultSlice(body, "default_acl")[0].(map[string]any)
		entry := faultSlice(faultMap(defaultACL, "acl"), "entries")[0].(map[string]any)
		entry["origin"] = "catalog_explicit"
		return decodeFaultCatalogBody(t, body)
	case "schema_select_privilege":
		body := faultObject(t, filepath.Join(root, "golden", "catalog-projection-body-v1.json"))
		entry := faultSlice(faultMap(faultMap(body, "schema"), "explicit_acl"), "entries")[0].(map[string]any)
		entry["privileges"] = []any{"SELECT"}
		return decodeFaultCatalogBody(t, body)
	case "password_setting":
		binding := faultObject(t, filepath.Join(root, "golden", "authority-binding-v1.json"))
		projection := faultMap(faultMap(binding, "expected_projections"), "connected_session")
		role := faultSlice(projection, "roles")[0].(map[string]any)
		role["config"] = []any{"password=fixture-secret"}
		return decodeFaultAuthorityBinding(t, binding)
	default:
		t.Fatalf("checked-in mutation %q has no Go executor", mutation)
		return nil
	}
}

func TestProjectionTerminalReviewDirectFaults(t *testing.T) {
	t.Parallel()
	root := filepath.Join(migrationRoot(t), "fixtures", "projection", "golden")

	t.Run("security_epoch_uint32", func(t *testing.T) {
		binding := faultObject(t, filepath.Join(root, "authority-binding-v1.json"))
		binding["security_epoch"] = float64(4294967296)
		if err := decodeFaultAuthorityBinding(t, binding); err == nil {
			t.Fatal("security_epoch above uint32 was accepted")
		}
	})

	t.Run("authority_nullable_and_identity", func(t *testing.T) {
		projection := minimalAuthorityProjection(AuthorityPhaseConnectedSession)
		empty := ""
		projection.Roles = []RoleProjection{{Name: MigrationOwnerRole, ConnectionLimitInt32Decimal: "-1", ValidUntil: &empty, Config: []string{}}}
		if err := projection.Validate(); err == nil {
			t.Fatal("non-null empty valid_until was accepted")
		}
		for _, field := range []string{"icu_locale", "icu_rules", "collation_version"} {
			projection = minimalAuthorityProjection(AuthorityPhaseConnectedSession)
			value := "non-null"
			switch field {
			case "icu_locale":
				projection.ICULocale = &value
			case "icu_rules":
				projection.ICURules = &value
			case "collation_version":
				projection.CollationVersion = &value
			}
			if err := projection.Validate(); err == nil {
				t.Fatalf("non-null %s was accepted", field)
			}
		}
		projection = minimalAuthorityProjection(AuthorityPhaseConnectedSession)
		projection.EffectiveCreate[""] = true
		if err := projection.Validate(); err == nil {
			t.Fatal("empty effective privilege identity was accepted")
		}
	})

	t.Run("reachability_witness", func(t *testing.T) {
		depth := uint32(1)
		emptyWitness := []string{}
		privilege := ReachabilityPrivilegeProjection{PrivilegeKind: "member", Reachable: true, MinDepth: &depth, CanonicalWitness: &emptyWitness, EdgeCount: 1}
		projection := ReachabilityProjection{Role: "role", Member: "member", Privileges: []ReachabilityPrivilegeProjection{privilege, {PrivilegeKind: "usage"}, {PrivilegeKind: "set"}}}
		if err := projection.Validate(); err == nil {
			t.Fatal("empty reachable witness was accepted")
		}
		unsorted := []string{"z", "a"}
		projection.Privileges[0].CanonicalWitness = &unsorted
		if err := projection.Validate(); err == nil {
			t.Fatal("unsorted canonical witness was accepted")
		}
	})

	t.Run("default_acl_closed_kind_and_schema", func(t *testing.T) {
		var body CatalogProjectionBody
		if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "catalog-projection-body-v1.json")), &body); err != nil {
			t.Fatal(err)
		}
		body.DefaultACL[0].ObjectKind = "type"
		if err := body.Validate(); err == nil {
			t.Fatal("default ACL type outside A2.1a closure was accepted")
		}
		if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "catalog-projection-body-v1.json")), &body); err != nil {
			t.Fatal(err)
		}
		body.DefaultACL[0].Schema = nil
		if err := body.Validate(); err == nil {
			t.Fatal("default ACL without cloud_agents schema was accepted")
		}
	})

	t.Run("terminal_error_and_ids", func(t *testing.T) {
		var terminal AttemptTerminalState
		if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "attempt-terminal-state-v1.json")), &terminal); err != nil {
			t.Fatal(err)
		}
		empty, unknown := "", "UNBOUNDED_DATABASE_ERROR"
		terminal.StableErrorCode = &empty
		if err := terminal.Validate(3); err == nil {
			t.Fatal("non-null empty stable error was accepted")
		}
		terminal.StableErrorCode = &unknown
		if err := terminal.Validate(3); err == nil {
			t.Fatal("unknown stable error was accepted")
		}
		terminal.StableErrorCode = nil
		terminal.MigrationID = "1"
		if err := terminal.Validate(3); err == nil {
			t.Fatal("terminal non-profile migration ID was accepted")
		}
		var intermediate StatementIntermediateState
		if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "intermediate-state-v1.json")), &intermediate); err != nil {
			t.Fatal(err)
		}
		intermediate.MigrationID = "1"
		if err := intermediate.Validate(); err == nil {
			t.Fatal("intermediate non-profile migration ID was accepted")
		}
	})

	t.Run("nested_optional_strings", func(t *testing.T) {
		identity := ObjectIdentityProjection{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: TypeIdentity{Schema: "cloud_agents", Name: "jobs"}, Name: "trigger", OwningConstraint: &ConstraintObjectIdentity{Kind: "constraint"}}}
		if err := identity.Validate(); err == nil {
			t.Fatal("incomplete trigger owning constraint was accepted")
		}
		empty := ""
		body := minimalCatalogBody()
		body.DeniedObjects = []DeniedObjectProjection{{Object: ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: "unknown"}}, Owner: &empty, ReasonCode: "undeclared_object"}}
		if err := body.Validate(); err == nil {
			t.Fatal("non-null empty denied owner was accepted")
		}
		body.DeniedObjects[0].Owner = nil
		body.DeniedObjects[0].DependencyKind = &empty
		if err := body.Validate(); err == nil {
			t.Fatal("non-null empty denied dependency kind was accepted")
		}
		var transition ExpectedStatementTransition
		if _, err := DecodeStrict(mustRead(t, filepath.Join(root, "expected-statement-transition-v1.json")), &transition); err != nil {
			t.Fatal(err)
		}
		transition.ControlPlaneDelta[0].Grantee = &empty
		if err := transition.Validate(); err == nil {
			t.Fatal("non-null empty transition grantee was accepted")
		}
	})

	t.Run("executable_catalog_cross_bindings", func(t *testing.T) {
		valid := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
		if _, err := DecodeCatalogContract(mustJSON(t, valid)); err != nil {
			t.Fatalf("valid executable catalog: %v", err)
		}
		mismatch := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
		mismatch.DeclaredObjectIdentities = []ObjectIdentityProjection{{Schema: &SchemaObjectIdentity{Kind: "schema", Name: "cloud_agents"}}}
		if _, err := DecodeCatalogContract(mustJSON(t, mismatch)); err == nil {
			t.Fatal("top-level/expected declared closure mismatch was accepted")
		}
		wrongSource := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
		wrongMigrationID := "000002"
		wrongSource.SourceDescriptors[0].Statements[0].ExpectedTransition.CatalogBefore.Scope.MigrationID = &wrongMigrationID
		if _, err := DecodeCatalogContract(mustJSON(t, wrongSource)); err == nil {
			t.Fatal("transition scope/source migration mismatch was accepted")
		}
		wrongIndex := twoStatementExecutableCatalog(t)
		wrongThrough := uint32(1)
		wrongIndex.SourceDescriptors[0].Statements[1].ExpectedTransition.CatalogBefore.Scope.ThroughStatementIndex = &wrongThrough
		if _, err := DecodeCatalogContract(mustJSON(t, wrongIndex)); err == nil {
			t.Fatal("transition scope/statement index mismatch was accepted")
		}
		wrongFinal := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
		wrongFinal.SourceDescriptors[0].Statements[0].ExpectedTransition.CatalogAfter.Scope.DeclaredObjects = []ObjectIdentityProjection{{Schema: &SchemaObjectIdentity{Kind: "schema", Name: "cloud_agents"}}}
		if _, err := DecodeCatalogContract(mustJSON(t, wrongFinal)); err == nil {
			t.Fatal("final transition/catalog declared closure mismatch was accepted")
		}
	})
}

func twoStatementExecutableCatalog(t *testing.T) CatalogContract {
	t.Helper()
	contract := minimalCatalogContract(t, "PUBLISHED_IMMUTABLE", "IMPLEMENTED")
	finalStatement := contract.SourceDescriptors[0].Statements[0]
	migrationID := contract.SourceDescriptors[0].MigrationID
	through := uint32(0)
	prefix := CatalogStateDigestRef{
		Scope:     ProjectionScope{ScopeKind: "statement_prefix", MigrationID: &migrationID, ThroughStatementIndex: &through, DeclaredObjects: []ObjectIdentityProjection{}},
		StateKind: "schema_present",
		Digest:    projectionTestDigest,
	}
	first := finalStatement
	first.ExpectedTransition.CatalogAfter = prefix
	second := finalStatement
	second.Index = 1
	second.Start = 1
	second.End = 2
	second.ExpectedTransition.CatalogBefore = prefix
	contract.SourceDescriptors[0].Statements = []SQLStatementDescriptor{first, second}
	return contract
}

func faultObject(t *testing.T, path string) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(mustRead(t, path), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func cloneFaultObject(t *testing.T, object map[string]any) map[string]any {
	t.Helper()
	var clone map[string]any
	if err := json.Unmarshal(faultJSON(t, object), &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func faultMap(object map[string]any, field string) map[string]any {
	return object[field].(map[string]any)
}

func faultSlice(object map[string]any, field string) []any {
	return object[field].([]any)
}

func faultJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeFaultAuthorityBinding(t *testing.T, object map[string]any) error {
	t.Helper()
	_, err := DecodeAuthorityBinding(faultJSON(t, object))
	return err
}

func decodeFaultCatalogState(t *testing.T, object map[string]any) error {
	t.Helper()
	var state CatalogStateProjection
	if _, err := DecodeStrict(faultJSON(t, object), &state); err != nil {
		return err
	}
	return state.Validate()
}

func decodeFaultCatalogBody(t *testing.T, object map[string]any) error {
	t.Helper()
	var body CatalogProjectionBody
	if _, err := DecodeStrict(faultJSON(t, object), &body); err != nil {
		return err
	}
	return body.Validate()
}

func decodeFaultObjectIdentity(t *testing.T, object map[string]any) error {
	t.Helper()
	var identity ObjectIdentityProjection
	if _, err := DecodeStrict(faultJSON(t, object), &identity); err != nil {
		return err
	}
	return identity.Validate()
}

func decodeFaultTransition(t *testing.T, object map[string]any) error {
	t.Helper()
	var transition ExpectedStatementTransition
	if _, err := DecodeStrict(faultJSON(t, object), &transition); err != nil {
		return err
	}
	return transition.Validate()
}

func decodeFaultIntermediate(t *testing.T, object map[string]any) error {
	t.Helper()
	var state StatementIntermediateState
	if _, err := DecodeStrict(faultJSON(t, object), &state); err != nil {
		return err
	}
	return state.Validate()
}

func decodeFaultTerminal(t *testing.T, object map[string]any) error {
	t.Helper()
	var state AttemptTerminalState
	if _, err := DecodeStrict(faultJSON(t, object), &state); err != nil {
		return err
	}
	return state.Validate(3)
}
